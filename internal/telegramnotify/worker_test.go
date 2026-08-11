package telegramnotify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendMediaDigest(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/bottest-token/sendMessage" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	worker := New(nil, "test-token", "zhyvoappbot", time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.apiBase = server.URL
	worker.client = server.Client()
	err := worker.send(context.Background(), 123, "media_uploaded", payload{RoomName: "Свято", RoomSlug: "ABC234", Actor: "Оля", Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(received["text"].(string), "3 нових файлів") {
		t.Fatalf("unexpected text: %v", received["text"])
	}
	markup := received["reply_markup"].(map[string]any)
	if !strings.Contains(string(mustJSON(t, markup)), "startapp=room_ABC234") {
		t.Fatalf("room deep link missing: %v", markup)
	}
}

func TestSendExpiryReminder(t *testing.T) {
	for _, test := range []struct {
		name  string
		hours int
		want  string
	}{
		{name: "six hours", hours: 6, want: "менше 6 годин"},
		{name: "one hour", hours: 1, want: "менш ніж за годину"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var received map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
					t.Fatal(err)
				}
				_, _ = response.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()

			worker := New(nil, "test-token", "zhyvoappbot", time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
			worker.apiBase = server.URL
			worker.client = server.Client()
			if err := worker.send(context.Background(), 123, "room_expiry", payload{RoomName: "Свято", RoomSlug: "ABC234", HoursRemaining: test.hours}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(received["text"].(string), test.want) {
				t.Fatalf("unexpected text: %v", received["text"])
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
