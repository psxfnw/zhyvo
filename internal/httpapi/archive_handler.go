package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photodrop/internal/roomarchive"
)

type archiveHandler struct {
	service *roomarchive.Service
}

func (handler archiveHandler) request(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	job, err := handler.service.Request(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	status := http.StatusAccepted
	if job.Status == "ready" {
		status = http.StatusOK
	}
	writeJSON(response, status, map[string]any{"archive": job})
}

func (handler archiveHandler) get(response http.ResponseWriter, request *http.Request) {
	principal, archiveID, ok := handler.principalAndArchiveID(response, request)
	if !ok {
		return
	}
	job, err := handler.service.Get(request.Context(), principal, archiveID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"archive": job})
}

func (handler archiveHandler) download(response http.ResponseWriter, request *http.Request) {
	principal, archiveID, ok := handler.principalAndArchiveID(response, request)
	if !ok {
		return
	}
	download, err := handler.service.Download(request.Context(), principal, archiveID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, download)
}

func (handler archiveHandler) principalAndArchiveID(response http.ResponseWriter, request *http.Request) (uuid.UUID, uuid.UUID, bool) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return uuid.Nil, uuid.Nil, false
	}
	archiveID, err := uuid.Parse(chi.URLParam(request, "archiveID"))
	if err != nil {
		writeAPIError(response, request, http.StatusNotFound, "ARCHIVE_NOT_FOUND", "Archive not found")
		return uuid.Nil, uuid.Nil, false
	}
	return principal.IdentityID, archiveID, true
}

func (handler archiveHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, roomarchive.ErrNotMember):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_MEMBERSHIP_REQUIRED", "Join the room before downloading it")
	case errors.Is(err, roomarchive.ErrExpired):
		writeAPIError(response, request, http.StatusGone, "ROOM_EXPIRED", "Room has expired")
	case errors.Is(err, roomarchive.ErrEmpty):
		writeAPIError(response, request, http.StatusConflict, "ROOM_GALLERY_EMPTY", "Room has no media to archive")
	case errors.Is(err, roomarchive.ErrNotFound):
		writeAPIError(response, request, http.StatusNotFound, "ARCHIVE_NOT_FOUND", "Archive not found")
	case errors.Is(err, roomarchive.ErrNotReady):
		writeAPIError(response, request, http.StatusConflict, "ARCHIVE_NOT_READY", "Archive is still being prepared")
	default:
		writeAPIError(response, request, http.StatusBadGateway, "ARCHIVE_ERROR", "Could not prepare room archive")
	}
}
