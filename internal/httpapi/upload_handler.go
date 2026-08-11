package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photodrop/internal/media"
)

type uploadHandler struct {
	service *media.Service
}

type initiateUploadRequest struct {
	Filename   string     `json:"filename"`
	MIMEType   string     `json:"mime_type"`
	SizeBytes  int64      `json:"size_bytes"`
	CapturedAt *time.Time `json:"captured_at"`
}

type partURLsRequest struct {
	PartNumbers []int `json:"part_numbers"`
}

type completeUploadRequest struct {
	Parts []media.CompletedPart `json:"parts"`
}

type roomCoverRequest struct {
	MediaID uuid.UUID `json:"media_id"`
}

func (handler uploadHandler) initiate(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	idempotencyKey, err := uuid.Parse(request.Header.Get("Idempotency-Key"))
	if err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be a UUID")
		return
	}
	var input initiateUploadRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}

	upload, err := handler.service.Initiate(request.Context(), principal.IdentityID, idempotencyKey, chi.URLParam(request, "slug"), media.InitiateInput{
		Filename: input.Filename, MIMEType: input.MIMEType, SizeBytes: input.SizeBytes, CapturedAt: input.CapturedAt,
	})
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"upload": upload, "media_id": upload.MediaID})
}

func (handler uploadHandler) parts(response http.ResponseWriter, request *http.Request) {
	principal, uploadID, ok := handler.principalAndUploadID(response, request)
	if !ok {
		return
	}
	var input partURLsRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	urls, err := handler.service.PartURLs(request.Context(), principal, uploadID, input.PartNumbers)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"parts": urls})
}

func (handler uploadHandler) complete(response http.ResponseWriter, request *http.Request) {
	principal, uploadID, ok := handler.principalAndUploadID(response, request)
	if !ok {
		return
	}
	var input completeUploadRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	result, err := handler.service.Complete(request.Context(), principal, uploadID, input.Parts)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"media": result})
}

func (handler uploadHandler) abort(response http.ResponseWriter, request *http.Request) {
	principal, uploadID, ok := handler.principalAndUploadID(response, request)
	if !ok {
		return
	}
	if err := handler.service.Abort(request.Context(), principal, uploadID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler uploadHandler) gallery(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeAPIError(response, request, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "limit must be an integer")
			return
		}
		limit = parsed
	}
	page, err := handler.service.Gallery(
		request.Context(),
		principal.IdentityID,
		chi.URLParam(request, "slug"),
		limit,
		request.URL.Query().Get("cursor"),
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (handler uploadHandler) download(response http.ResponseWriter, request *http.Request) {
	principal, mediaID, ok := handler.principalAndMediaID(response, request)
	if !ok {
		return
	}
	download, err := handler.service.Download(request.Context(), principal, mediaID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, download)
}

func (handler uploadHandler) deleteMedia(response http.ResponseWriter, request *http.Request) {
	principal, mediaID, ok := handler.principalAndMediaID(response, request)
	if !ok {
		return
	}
	if err := handler.service.Delete(request.Context(), principal, mediaID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler uploadHandler) favorite(response http.ResponseWriter, request *http.Request) {
	principal, mediaID, ok := handler.principalAndMediaID(response, request)
	if !ok {
		return
	}
	state, err := handler.service.SetFavorite(request.Context(), principal, mediaID, request.Method == http.MethodPut)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, state)
}

func (handler uploadHandler) setCover(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input roomCoverRequest
	if err := decodeJSON(response, request, &input); err != nil || input.MediaID == uuid.Nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "media_id must be a UUID")
		return
	}
	if err := handler.service.SetCover(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), input.MediaID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler uploadHandler) principalAndUploadID(response http.ResponseWriter, request *http.Request) (uuid.UUID, uuid.UUID, bool) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return uuid.Nil, uuid.Nil, false
	}
	uploadID, err := uuid.Parse(chi.URLParam(request, "uploadID"))
	if err != nil {
		writeAPIError(response, request, http.StatusNotFound, "UPLOAD_NOT_FOUND", "Upload not found")
		return uuid.Nil, uuid.Nil, false
	}
	return principal.IdentityID, uploadID, true
}

func (handler uploadHandler) principalAndMediaID(response http.ResponseWriter, request *http.Request) (uuid.UUID, uuid.UUID, bool) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return uuid.Nil, uuid.Nil, false
	}
	mediaID, err := uuid.Parse(chi.URLParam(request, "mediaID"))
	if err != nil {
		writeAPIError(response, request, http.StatusNotFound, "MEDIA_NOT_FOUND", "Media not found")
		return uuid.Nil, uuid.Nil, false
	}
	return principal.IdentityID, mediaID, true
}

func (handler uploadHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, media.ErrInvalidInput):
		writeAPIError(response, request, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, media.ErrRoomNotFound):
		writeAPIError(response, request, http.StatusNotFound, "ROOM_NOT_FOUND", "Room not found")
	case errors.Is(err, media.ErrRoomExpired):
		writeAPIError(response, request, http.StatusGone, "ROOM_EXPIRED", "Room has expired")
	case errors.Is(err, media.ErrNotMember):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_MEMBERSHIP_REQUIRED", "Join the room before uploading")
	case errors.Is(err, media.ErrUploadsClosed):
		writeAPIError(response, request, http.StatusConflict, "ROOM_UPLOADS_CLOSED", "Room no longer accepts uploads")
	case errors.Is(err, media.ErrRoomLimitReached):
		writeAPIError(response, request, http.StatusConflict, "ROOM_STORAGE_LIMIT_REACHED", "Room storage or file limit has been reached")
	case errors.Is(err, media.ErrUploadNotFound):
		writeAPIError(response, request, http.StatusNotFound, "UPLOAD_NOT_FOUND", "Upload not found")
	case errors.Is(err, media.ErrUploadExpired):
		writeAPIError(response, request, http.StatusGone, "UPLOAD_EXPIRED", "Upload session has expired")
	case errors.Is(err, media.ErrUploadCompleted):
		writeAPIError(response, request, http.StatusConflict, "UPLOAD_ALREADY_COMPLETED", "Upload has already completed")
	case errors.Is(err, media.ErrUploadNotReady):
		writeAPIError(response, request, http.StatusConflict, "UPLOAD_NOT_READY", "Object upload has not completed in storage")
	case errors.Is(err, media.ErrInvalidUploadParts):
		writeAPIError(response, request, http.StatusUnprocessableEntity, "INVALID_UPLOAD_PARTS", err.Error())
	case errors.Is(err, media.ErrUploadedSizeMismatch):
		writeAPIError(response, request, http.StatusUnprocessableEntity, "UPLOADED_SIZE_MISMATCH", "Uploaded object size does not match declared size")
	case errors.Is(err, media.ErrIdempotencyConflict):
		writeAPIError(response, request, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used for another request")
	case errors.Is(err, media.ErrMediaNotFound):
		writeAPIError(response, request, http.StatusNotFound, "MEDIA_NOT_FOUND", "Media not found")
	case errors.Is(err, media.ErrMediaNotReady):
		writeAPIError(response, request, http.StatusConflict, "MEDIA_NOT_READY", "Media is not ready")
	case errors.Is(err, media.ErrRoomOwnerRequired):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_OWNER_REQUIRED", "Only the room owner can perform this action")
	case errors.Is(err, media.ErrMediaAccessDenied):
		writeAPIError(response, request, http.StatusForbidden, "MEDIA_DELETE_FORBIDDEN", "Only the uploader or room owner can delete this media")
	default:
		writeAPIError(response, request, http.StatusBadGateway, "STORAGE_ERROR", "Object storage operation failed")
	}
}
