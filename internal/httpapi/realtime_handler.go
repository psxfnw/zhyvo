package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"photodrop/internal/auth"
	"photodrop/internal/realtime"
)

const realtimeBatchSize = 100

type realtimeHandler struct {
	service *realtime.Service
	broker  *realtime.Broker
	auth    *auth.Service
}

func (handler realtimeHandler) events(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeAPIError(response, request, http.StatusInternalServerError, "STREAMING_UNAVAILABLE", "Streaming is unavailable")
		return
	}

	roomID, currentCursor, err := handler.service.ResolveRoom(request.Context(), principal.IdentityID, chi.URLParam(request, "slug"))
	if errors.Is(err, realtime.ErrNotMember) {
		writeAPIError(response, request, http.StatusForbidden, "ROOM_ACCESS_DENIED", "Room membership is required")
		return
	}
	if err != nil {
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}

	cursor := currentCursor
	if rawCursor := request.Header.Get("Last-Event-ID"); rawCursor != "" {
		cursor, err = strconv.ParseInt(rawCursor, 10, 64)
		if err != nil || cursor < 0 {
			writeAPIError(response, request, http.StatusUnprocessableEntity, "INVALID_EVENT_CURSOR", "Last-Event-ID must be a positive integer")
			return
		}
	}
	wake, unsubscribe := handler.broker.Subscribe(roomID)
	defer unsubscribe()

	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	if err := writeSSE(response, cursor, "ready", map[string]string{"type": "ready"}); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	pending := true
	for {
		if !pending {
			select {
			case <-request.Context().Done():
				return
			case <-wake:
			case <-heartbeat.C:
				_, _ = fmt.Fprint(response, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
		pending = false
		active, err := handler.auth.SessionIsActive(request.Context(), principal)
		if err != nil {
			return
		}
		if !active {
			_ = writeSSE(response, cursor, "access_revoked", map[string]string{"type": "access_revoked"})
			flusher.Flush()
			return
		}

		for {
			events, err := handler.service.EventsAfter(request.Context(), principal.IdentityID, roomID, cursor, realtimeBatchSize)
			if errors.Is(err, realtime.ErrNotMember) {
				_ = writeSSE(response, cursor, "access_revoked", map[string]string{"type": "access_revoked"})
				flusher.Flush()
				return
			}
			if err != nil {
				return
			}
			for _, event := range events {
				if err := writeSSE(response, event.ID, "room", event); err != nil {
					return
				}
				cursor = event.ID
			}
			if len(events) > 0 {
				flusher.Flush()
			}
			if len(events) < realtimeBatchSize {
				break
			}
		}
	}
}

func writeSSE(response http.ResponseWriter, id int64, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(response, "id: %d\nevent: %s\ndata: %s\n\n", id, eventType, data)
	return err
}
