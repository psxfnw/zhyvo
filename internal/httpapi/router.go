package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/auth"
	"photodrop/internal/media"
	"photodrop/internal/objectstore"
	"photodrop/internal/room"
)

type Dependencies struct {
	DB                  *pgxpool.Pool
	Store               *objectstore.Store
	AuthService         *auth.Service
	Tokens              *auth.TokenManager
	RoomService         *room.Service
	MediaService        *media.Service
	TelegramBotToken    string
	TelegramInitDataTTL time.Duration
}

func NewRouter(dependencies Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	anonymousLimiter := newRateLimiter(12, 5)
	refreshLimiter := newRateLimiter(30, 10)
	joinLimiter := newRateLimiter(6, 3)
	uploadLimiter := newRateLimiter(60, 10)

	router.Get("/health/live", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	})

	router.Get("/health/ready", func(response http.ResponseWriter, request *http.Request) {
		if err := dependencies.DB.Ping(request.Context()); err != nil {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "dependency": "postgres"})
			return
		}
		if err := dependencies.Store.Ready(request.Context()); err != nil {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "dependency": "object_storage"})
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
	})

	authAPI := authHandler{service: dependencies.AuthService, telegramBotToken: dependencies.TelegramBotToken, telegramInitDataTTL: dependencies.TelegramInitDataTTL}
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.With(anonymousLimiter.middleware(clientIPKey)).Post("/anonymous", authAPI.createAnonymous)
		router.With(anonymousLimiter.middleware(clientIPKey)).Post("/telegram", authAPI.createTelegram)
		router.With(refreshLimiter.middleware(clientIPKey)).Post("/refresh", authAPI.refresh)
		router.Group(func(router chi.Router) {
			router.Use(requireAuth(dependencies.Tokens, dependencies.AuthService))
			router.Get("/me", authAPI.me)
			router.Delete("/session", authAPI.logout)
		})
	})

	roomAPI := roomHandler{service: dependencies.RoomService}
	uploadAPI := uploadHandler{service: dependencies.MediaService}
	router.Get("/api/v1/rooms/{slug}/preview", roomAPI.preview)
	router.Group(func(router chi.Router) {
		router.Use(requireAuth(dependencies.Tokens, dependencies.AuthService))
		router.Post("/api/v1/rooms", roomAPI.create)
		router.Get("/api/v1/rooms", roomAPI.list)
		router.With(joinLimiter.middleware(identityKey)).Post("/api/v1/rooms/{slug}/join", roomAPI.join)
		router.Get("/api/v1/rooms/{slug}", roomAPI.get)
		router.Get("/api/v1/rooms/{slug}/members", roomAPI.members)
		router.Delete("/api/v1/rooms/{slug}/members/{identityID}", roomAPI.removeMember)
		router.Delete("/api/v1/rooms/{slug}/blocked-members/{identityID}", roomAPI.unblockMember)
		router.Post("/api/v1/rooms/{slug}/ownership", roomAPI.transferOwnership)
		router.Get("/api/v1/rooms/{slug}/activity", roomAPI.activity)
		router.Patch("/api/v1/rooms/{slug}", roomAPI.update)
		router.Delete("/api/v1/rooms/{slug}", roomAPI.delete)
		router.With(uploadLimiter.middleware(identityKey)).Post("/api/v1/rooms/{slug}/uploads", uploadAPI.initiate)
		router.With(uploadLimiter.middleware(identityKey)).Post("/api/v1/uploads/{uploadID}/parts", uploadAPI.parts)
		router.Post("/api/v1/uploads/{uploadID}/complete", uploadAPI.complete)
		router.Delete("/api/v1/uploads/{uploadID}", uploadAPI.abort)
		router.Get("/api/v1/rooms/{slug}/media", uploadAPI.gallery)
		router.Post("/api/v1/media/{mediaID}/download-url", uploadAPI.download)
		router.Delete("/api/v1/media/{mediaID}", uploadAPI.deleteMedia)
	})

	router.NotFound(func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusNotFound, map[string]any{
			"error": map[string]string{
				"code":       "NOT_FOUND",
				"message":    "Resource not found",
				"request_id": middleware.GetReqID(request.Context()),
			},
		})
	})

	return router
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}

func writeAPIError(response http.ResponseWriter, request *http.Request, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{
			"code":       code,
			"message":    message,
			"request_id": middleware.GetReqID(request.Context()),
		},
	})
}
