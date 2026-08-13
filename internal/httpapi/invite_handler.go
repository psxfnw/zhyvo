package httpapi

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"photodrop/internal/room"
)

type inviteHandler struct {
	rooms *room.Service
}

var invitePage = template.Must(template.New("invite").Parse(`<!doctype html>
<html lang="uk">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.RoomName}} — Zhyvo</title>
  <meta name="description" content="Приєднуйтеся до спільної галереї події у Zhyvo. Фото й відео в оригінальній якості.">
  <meta property="og:type" content="website">
  <meta property="og:site_name" content="Zhyvo">
  <meta property="og:title" content="{{.RoomName}} — спільна галерея">
  <meta property="og:description" content="Приєднуйтеся до кімнати у Zhyvo та діліться оригінальними фото й відео.">
  <meta property="og:image" content="{{.Origin}}/zhyvo-room-preview.png">
  <meta property="og:image:width" content="1200">
  <meta property="og:image:height" content="630">
  <meta property="og:url" content="{{.CanonicalURL}}">
  <meta name="twitter:card" content="summary_large_image">
  <link rel="canonical" href="{{.CanonicalURL}}">
  <style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f5f5f7;color:#1d1d1f;font:16px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.card{width:min(440px,calc(100% - 40px));padding:32px;border:1px solid #fff;border-radius:28px;background:rgba(255,255,255,.86);box-shadow:0 24px 70px rgba(35,68,115,.14);text-align:center}.mark{width:70px;border-radius:18px}h1{margin:20px 0 8px;font-size:30px;letter-spacing:-.04em}p{color:#6e6e73;line-height:1.5}a{margin-top:12px;padding:14px 22px;display:inline-block;border-radius:999px;background:#0071e3;color:#fff;font-weight:700;text-decoration:none}</style>
</head>
<body><main class="card"><img class="mark" src="{{.Origin}}/pwa-192x192.png" alt=""><h1>{{.RoomName}}</h1><p>Приєднуйтеся до спільної галереї події.</p><a href="{{.WebURL}}">Приєднатися до кімнати</a></main></body>
</html>`))

func isLinkPreviewCrawler(userAgent string) bool {
	userAgent = strings.ToLower(userAgent)
	for _, marker := range []string{"telegrambot", "twitterbot", "facebookexternalhit", "whatsapp", "slackbot", "discordbot", "linkedinbot"} {
		if strings.Contains(userAgent, marker) {
			return true
		}
	}
	return false
}

func (handler inviteHandler) show(response http.ResponseWriter, request *http.Request) {
	token := chi.URLParam(request, "slug")
	preview, err := handler.rooms.Preview(request.Context(), token)
	startParam := "room_"
	if len(token) > 12 {
		managed, managedErr := handler.rooms.InvitePreview(request.Context(), token)
		preview, err = managed.Preview, managedErr
		startParam = "invite_"
	}
	if errors.Is(err, room.ErrNotFound) || errors.Is(err, room.ErrInviteNotFound) || errors.Is(err, room.ErrExpired) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "Unable to open invitation", http.StatusBadGateway)
		return
	}
	proto := request.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
		if request.TLS != nil {
			proto = "https"
		}
	}
	origin := proto + "://" + request.Host
	webPath := "/r/" + token
	if startParam == "invite_" {
		webPath = "/i/" + token
	}
	if !isLinkPreviewCrawler(request.UserAgent()) {
		http.Redirect(response, request, webPath, http.StatusSeeOther)
		return
	}
	data := struct {
		RoomName     string
		Origin       string
		CanonicalURL string
		WebURL       string
	}{preview.Name, origin, origin + request.URL.Path, origin + webPath}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	_ = invitePage.Execute(response, data)
}
