package media

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGalleryCursorRoundTrip(t *testing.T) {
	want := galleryCursor{
		SortAt: time.Date(2026, time.July, 17, 12, 34, 56, 123456000, time.UTC),
		ID:     uuid.New(),
	}
	encoded := encodeGalleryCursor(want)
	got, err := decodeGalleryCursor(encoded)
	if err != nil {
		t.Fatalf("decodeGalleryCursor() error = %v", err)
	}
	if !got.SortAt.Equal(want.SortAt) || got.ID != want.ID {
		t.Fatalf("cursor round trip = %#v, want %#v", got, want)
	}
}

func TestGalleryCursorRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"invalid", "", "YWJj"} {
		if _, err := decodeGalleryCursor(input); err == nil {
			t.Fatalf("decodeGalleryCursor(%q) succeeded, want error", input)
		}
	}
}
