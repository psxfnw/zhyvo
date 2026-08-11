package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteSSE(t *testing.T) {
	response := httptest.NewRecorder()
	if err := writeSSE(response, 42, "room", map[string]string{"type": "media_ready"}); err != nil {
		t.Fatal(err)
	}
	want := "id: 42\nevent: room\ndata: {\"type\":\"media_ready\"}\n\n"
	if got := response.Body.String(); got != want {
		t.Fatalf("SSE frame = %q, want %q", got, want)
	}
	if strings.Contains(response.Body.String(), "\r") {
		t.Fatal("SSE frame should use LF delimiters")
	}
}
