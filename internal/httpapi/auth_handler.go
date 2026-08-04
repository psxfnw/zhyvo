package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"photodrop/internal/auth"
)

type authHandler struct {
	service             *auth.Service
	telegramBotToken    string
	telegramInitDataTTL time.Duration
}

type telegramRequest struct {
	InitData string `json:"init_data"`
}

type anonymousRequest struct {
	DisplayName string `json:"display_name"`
	ClientType  string `json:"client_type"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	Identity              auth.Identity `json:"identity"`
	AccessToken           string        `json:"access_token"`
	AccessTokenExpiresIn  int64         `json:"access_token_expires_in"`
	AccessTokenExpiresAt  time.Time     `json:"access_token_expires_at"`
	RefreshToken          string        `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time     `json:"refresh_token_expires_at"`
}

func (handler authHandler) createAnonymous(response http.ResponseWriter, request *http.Request) {
	var input anonymousRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}

	result, err := handler.service.CreateAnonymous(request.Context(), input.DisplayName, input.ClientType)
	if errors.Is(err, auth.ErrInvalidInput) {
		writeAPIError(response, request, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	if err != nil {
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	writeJSON(response, http.StatusCreated, newTokenResponse(result))
}

func (handler authHandler) createTelegram(response http.ResponseWriter, request *http.Request) {
	if handler.telegramBotToken == "" {
		writeAPIError(response, request, http.StatusServiceUnavailable, "TELEGRAM_AUTH_NOT_CONFIGURED", "Telegram authentication is not configured")
		return
	}
	var input telegramRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}
	user, err := auth.ValidateTelegramInitData(input.InitData, handler.telegramBotToken, handler.telegramInitDataTTL, time.Now().UTC())
	if errors.Is(err, auth.ErrExpiredTelegramData) {
		writeAPIError(response, request, http.StatusUnauthorized, "TELEGRAM_INIT_DATA_EXPIRED", "Telegram authorization data has expired; reopen the Mini App")
		return
	}
	if err != nil {
		writeAPIError(response, request, http.StatusUnauthorized, "INVALID_TELEGRAM_INIT_DATA", "Telegram authorization data is invalid")
		return
	}
	result, err := handler.service.CreateTelegram(request.Context(), user)
	if err != nil {
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	writeJSON(response, http.StatusCreated, newTokenResponse(result))
}

func (handler authHandler) refresh(response http.ResponseWriter, request *http.Request) {
	var input refreshRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeAPIError(response, request, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid")
		return
	}

	result, err := handler.service.Refresh(request.Context(), input.RefreshToken)
	if errors.Is(err, auth.ErrInvalidRefreshToken) {
		writeAPIError(response, request, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Refresh token is invalid or expired")
		return
	}
	if err != nil {
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	writeJSON(response, http.StatusOK, newTokenResponse(result))
}

func (handler authHandler) me(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	identity, err := handler.service.GetIdentity(request.Context(), principal.IdentityID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAPIError(response, request, http.StatusUnauthorized, "IDENTITY_NOT_FOUND", "Identity no longer exists")
		return
	}
	if err != nil {
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"identity": identity})
}

func (handler authHandler) logout(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromContext(request.Context())
	if !ok {
		writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
		return
	}
	if err := handler.service.Revoke(request.Context(), principal.SessionID, principal.IdentityID); err != nil {
		writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func newTokenResponse(result auth.SessionTokens) tokenResponse {
	seconds := int64(time.Until(result.AccessTokenExpiry).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return tokenResponse{
		Identity:              result.Identity,
		AccessToken:           result.AccessToken,
		AccessTokenExpiresIn:  seconds,
		AccessTokenExpiresAt:  result.AccessTokenExpiry,
		RefreshToken:          result.RefreshToken,
		RefreshTokenExpiresAt: result.RefreshExpiry,
	}
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
