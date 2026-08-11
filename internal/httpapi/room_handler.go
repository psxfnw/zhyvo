package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photodrop/internal/room"
)

type roomHandler struct {
	service *room.Service
}

type createRoomRequest struct {
	Name         string            `json:"name"`
	LifetimeDays int               `json:"lifetime_days"`
	Access       roomAccessRequest `json:"access"`
}

type roomAccessRequest struct {
	Mode   string `json:"mode"`
	Secret string `json:"secret"`
}

type joinRoomRequest struct {
	Secret string `json:"secret"`
}

type updateRoomRequest struct {
	Name             *string            `json:"name"`
	AcceptingUploads *bool              `json:"accepting_uploads"`
	AcceptingMembers *bool              `json:"accepting_members"`
	Access           *roomAccessRequest `json:"access"`
	LifetimeDays     *int               `json:"lifetime_days"`
}

type transferOwnershipRequest struct {
	IdentityID uuid.UUID `json:"identity_id"`
}

type updateNotificationsRequest struct {
	TelegramEnabled bool `json:"telegram_enabled"`
}

type createInviteRequest struct {
	Permission string `json:"permission"`
}

func (handler roomHandler) create(response http.ResponseWriter, request *http.Request) {
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

	var input createRoomRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	created, err := handler.service.Create(request.Context(), principal.IdentityID, idempotencyKey, room.CreateInput{
		Name:         input.Name,
		LifetimeDays: input.LifetimeDays,
		AccessMode:   input.Access.Mode,
		Secret:       input.Access.Secret,
	})
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{
		"room":       created,
		"share_path": "/r/" + created.Slug,
	})
}

func (handler roomHandler) preview(response http.ResponseWriter, request *http.Request) {
	preview, err := handler.service.Preview(request.Context(), chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, preview)
}

func (handler roomHandler) list(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.List(request.Context(), principal.IdentityID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"rooms": result})
}

func (handler roomHandler) join(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input joinRoomRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	joined, err := handler.service.Join(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), input.Secret)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"room": joined})
}

func (handler roomHandler) invitePreview(response http.ResponseWriter, request *http.Request) {
	result, err := handler.service.InvitePreview(request.Context(), chi.URLParam(request, "token"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) joinInvite(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input joinRoomRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	joined, err := handler.service.JoinInvite(request.Context(), principal.IdentityID, chi.URLParam(request, "token"), input.Secret)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"room": joined})
}

func (handler roomHandler) invites(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.Invites(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) shareInvite(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.ShareInvite(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) createInvite(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input createInviteRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	result, err := handler.service.CreateInvite(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), input.Permission)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (handler roomHandler) revokeInvite(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	if err := handler.service.RevokeInvite(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), chi.URLParam(request, "token")); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler roomHandler) disableLegacyInvites(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	if err := handler.service.DisableLegacyInvites(request.Context(), principal.IdentityID, chi.URLParam(request, "slug")); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler roomHandler) get(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.Get(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"room": result})
}

func (handler roomHandler) members(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.Members(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) removeMember(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	memberID, err := uuid.Parse(chi.URLParam(request, "identityID"))
	if err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_IDENTITY_ID", "Identity ID must be a UUID")
		return
	}
	if err := handler.service.RemoveMember(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), memberID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler roomHandler) unblockMember(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	memberID, err := uuid.Parse(chi.URLParam(request, "identityID"))
	if err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_IDENTITY_ID", "Identity ID must be a UUID")
		return
	}
	if err := handler.service.UnblockMember(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), memberID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler roomHandler) transferOwnership(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input transferOwnershipRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	updated, err := handler.service.TransferOwnership(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), input.IdentityID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"room": updated})
}

func (handler roomHandler) activity(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.Activity(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"events": result})
}

func (handler roomHandler) recap(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.Recap(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) update(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input updateRoomRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	var access *room.AccessUpdate
	if input.Access != nil {
		access = &room.AccessUpdate{Mode: input.Access.Mode, Secret: input.Access.Secret}
	}
	updated, err := handler.service.Update(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), room.UpdateInput{
		Name: input.Name, AcceptingUploads: input.AcceptingUploads, AcceptingMembers: input.AcceptingMembers, Access: access, LifetimeDays: input.LifetimeDays,
	})
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"room": updated})
}

func (handler roomHandler) notifications(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	result, err := handler.service.Notifications(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) updateNotifications(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	var input updateNotificationsRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	result, err := handler.service.UpdateNotifications(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"), input.TelegramEnabled)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler roomHandler) delete(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	if err := handler.service.Delete(request.Context(), principal.IdentityID, chi.URLParam(request, "slug")); err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

func (handler roomHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, room.ErrInvalidInput):
		writeAPIError(response, request, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, room.ErrNotFound):
		writeAPIError(response, request, http.StatusNotFound, "ROOM_NOT_FOUND", "Room not found")
	case errors.Is(err, room.ErrInviteNotFound):
		writeAPIError(response, request, http.StatusNotFound, "INVITE_NOT_FOUND", "Invitation is invalid or has been revoked")
	case errors.Is(err, room.ErrExpired):
		writeAPIError(response, request, http.StatusGone, "ROOM_EXPIRED", "Room has expired")
	case errors.Is(err, room.ErrAccessDenied):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_ACCESS_DENIED", "PIN or password is incorrect")
	case errors.Is(err, room.ErrNotMember):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_MEMBERSHIP_REQUIRED", "Join the room before accessing it")
	case errors.Is(err, room.ErrOwnerRequired):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_OWNER_REQUIRED", "Only the room owner can perform this action")
	case errors.Is(err, room.ErrMemberBlocked):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_MEMBER_BLOCKED", "You were removed from this room by its owner")
	case errors.Is(err, room.ErrJoiningClosed):
		writeAPIError(response, request, http.StatusForbidden, "ROOM_JOINING_CLOSED", "The room is closed to new members")
	case errors.Is(err, room.ErrMemberNotFound):
		writeAPIError(response, request, http.StatusNotFound, "ROOM_MEMBER_NOT_FOUND", "Room member not found")
	case errors.Is(err, room.ErrMemberNotBlocked):
		writeAPIError(response, request, http.StatusNotFound, "ROOM_MEMBER_NOT_BLOCKED", "Room member is not blocked")
	case errors.Is(err, room.ErrCannotRemoveOwner):
		writeAPIError(response, request, http.StatusConflict, "ROOM_OWNER_CANNOT_BE_REMOVED", "Transfer ownership before removing the owner")
	case errors.Is(err, room.ErrIdempotencyConflict):
		writeAPIError(response, request, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used for another request")
	default:
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}
