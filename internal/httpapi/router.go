package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/auth"
	"photodrop/internal/media"
	"photodrop/internal/objectstore"
	"photodrop/internal/problemreport"
	"photodrop/internal/realtime"
	"photodrop/internal/room"
	"photodrop/internal/roomarchive"
)

type Dependencies struct {
	DB                   *pgxpool.Pool
	Store                *objectstore.Store
	AuthService          *auth.Service
	Tokens               *auth.TokenManager
	RoomService          *room.Service
	MediaService         *media.Service
	ArchiveService       *roomarchive.Service
	ProblemReportService *problemreport.Service
	RealtimeService      *realtime.Service
	RealtimeBroker       *realtime.Broker
	TelegramBotToken     string
	TelegramBotUsername  string
	TelegramInitDataTTL  time.Duration
	TelegramOIDC         *auth.TelegramOIDC
	Logger               *slog.Logger
}

func NewRouter(dependencies Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	anonymousLimiter := newRateLimiter(12, 5)
	refreshLimiter := newRateLimiter(30, 10)
	joinLimiter := newRateLimiter(6, 3)
	roomCreateLimiter := newRateLimiter(6, 3)
	roomCreateIPLimiter := newRateLimiter(30, 15)
	uploadLimiter := newRateLimiter(60, 10)
	reportLimiter := newRateLimiter(3, 2)

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

	logger := dependencies.Logger
	if logger == nil {
		logger = slog.Default()
	}
	authAPI := authHandler{service: dependencies.AuthService, telegramBotToken: dependencies.TelegramBotToken, telegramInitDataTTL: dependencies.TelegramInitDataTTL, telegramOIDC: dependencies.TelegramOIDC, logger: logger}
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.Get("/telegram/config", authAPI.telegramConfig)
		router.With(anonymousLimiter.middleware(clientIPKey)).Post("/anonymous", authAPI.createAnonymous)
		router.With(anonymousLimiter.middleware(clientIPKey)).Post("/telegram", authAPI.createTelegram)
		router.With(refreshLimiter.middleware(clientIPKey)).Post("/refresh", authAPI.refresh)
		router.Group(func(router chi.Router) {
			router.Use(requireAuth(dependencies.Tokens, dependencies.AuthService))
			router.Get("/me", authAPI.me)
			router.Post("/telegram/oidc", authAPI.linkTelegramOIDC)
			router.Post("/telegram/link-challenges", authAPI.createBrowserLink)
			router.Post("/telegram/link-challenges/status", authAPI.browserLinkStatus)
			router.Post("/telegram/link-challenges/approve", authAPI.approveBrowserLink)
			router.Post("/telegram/link-challenges/exchange", authAPI.exchangeBrowserLink)
			router.Delete("/session", authAPI.logout)
		})
	})

	roomAPI := roomHandler{service: dependencies.RoomService}
	uploadAPI := uploadHandler{service: dependencies.MediaService}
	archiveAPI := archiveHandler{service: dependencies.ArchiveService}
	realtimeAPI := realtimeHandler{service: dependencies.RealtimeService, broker: dependencies.RealtimeBroker, auth: dependencies.AuthService}
	problemReportAPI := problemReportHandler{service: dependencies.ProblemReportService}
	router.With(optionalAuth(dependencies.Tokens, dependencies.AuthService), reportLimiter.middleware(identityKey)).Post("/api/v1/problem-reports", problemReportAPI.create)
	router.Get("/api/v1/rooms/{slug}/preview", roomAPI.preview)
	router.Get("/api/v1/invites/{token}/preview", roomAPI.invitePreview)
	router.Get("/invite/{slug}", inviteHandler{rooms: dependencies.RoomService}.show)
	router.Group(func(router chi.Router) {
		router.Use(requireAuth(dependencies.Tokens, dependencies.AuthService))
		router.With(roomCreateLimiter.middleware(identityKey), roomCreateIPLimiter.middleware(clientIPKey)).Post("/api/v1/rooms", roomAPI.create)
		router.Get("/api/v1/rooms", roomAPI.list)
		router.With(joinLimiter.middleware(identityKey)).Post("/api/v1/rooms/{slug}/join", roomAPI.join)
		router.With(joinLimiter.middleware(identityKey)).Post("/api/v1/invites/{token}/join", roomAPI.joinInvite)
		router.Get("/api/v1/rooms/{slug}", roomAPI.get)
		router.Get("/api/v1/rooms/{slug}/members", roomAPI.members)
		router.Get("/api/v1/rooms/{slug}/invites", roomAPI.invites)
		router.Get("/api/v1/rooms/{slug}/share-invite", roomAPI.shareInvite)
		router.Post("/api/v1/rooms/{slug}/invites", roomAPI.createInvite)
		router.Delete("/api/v1/rooms/{slug}/invites/{token}", roomAPI.revokeInvite)
		router.Delete("/api/v1/rooms/{slug}/legacy-invite", roomAPI.disableLegacyInvites)
		router.Delete("/api/v1/rooms/{slug}/members/{identityID}", roomAPI.removeMember)
		router.Delete("/api/v1/rooms/{slug}/blocked-members/{identityID}", roomAPI.unblockMember)
		router.Post("/api/v1/rooms/{slug}/ownership", roomAPI.transferOwnership)
		router.Get("/api/v1/rooms/{slug}/activity", roomAPI.activity)
		router.Get("/api/v1/rooms/{slug}/recap", roomAPI.recap)
		router.Patch("/api/v1/rooms/{slug}", roomAPI.update)
		router.Get("/api/v1/rooms/{slug}/notifications", roomAPI.notifications)
		router.Patch("/api/v1/rooms/{slug}/notifications", roomAPI.updateNotifications)
		router.Delete("/api/v1/rooms/{slug}", roomAPI.delete)
		router.With(uploadLimiter.middleware(identityKey)).Post("/api/v1/rooms/{slug}/uploads", uploadAPI.initiate)
		router.With(uploadLimiter.middleware(identityKey)).Post("/api/v1/uploads/{uploadID}/parts", uploadAPI.parts)
		router.With(uploadLimiter.middleware(identityKey)).Post("/api/v1/uploads/{uploadID}/complete", uploadAPI.complete)
		router.Delete("/api/v1/uploads/{uploadID}", uploadAPI.abort)
		router.Get("/api/v1/rooms/{slug}/media", uploadAPI.gallery)
		router.Get("/api/v1/rooms/{slug}/highlights", uploadAPI.highlights)
		router.Get("/api/v1/rooms/{slug}/events", realtimeAPI.events)
		router.Put("/api/v1/media/{mediaID}/favorite", uploadAPI.favorite)
		router.Delete("/api/v1/media/{mediaID}/favorite", uploadAPI.favorite)
		router.Patch("/api/v1/media/{mediaID}", uploadAPI.updateCaption)
		router.Put("/api/v1/rooms/{slug}/cover", uploadAPI.setCover)
		router.Delete("/api/v1/rooms/{slug}/cover", uploadAPI.clearCover)
		router.Post("/api/v1/media/{mediaID}/download-url", uploadAPI.download)
		router.Delete("/api/v1/media/{mediaID}", uploadAPI.deleteMedia)
		router.Post("/api/v1/rooms/{slug}/archive", archiveAPI.request)
		router.Get("/api/v1/archives/{archiveID}", archiveAPI.get)
		router.Post("/api/v1/archives/{archiveID}/download-url", archiveAPI.download)
	})

	router.Group(func(router chi.Router) {
		router.Use(requireAuth(dependencies.Tokens, dependencies.AuthService))
		router.Use(requireAdmin(dependencies.ProblemReportService))
		router.Get("/api/v1/admin/stats", problemReportAPI.stats)
		router.Get("/api/v1/admin/problem-reports", problemReportAPI.list)
		router.Get("/api/v1/admin/problem-reports/{reportID}", problemReportAPI.get)
		router.Patch("/api/v1/admin/problem-reports/{reportID}", problemReportAPI.update)
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
