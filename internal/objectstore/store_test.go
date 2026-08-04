package objectstore

import (
	"strings"
	"testing"
)

func TestContentDisposition(t *testing.T) {
	result := contentDisposition("Фото з події.jpg")
	if !strings.Contains(result, `filename="____ _ _____.jpg"`) {
		t.Fatalf("contentDisposition() fallback is unsafe: %q", result)
	}
	if !strings.Contains(result, "filename*=UTF-8''") || !strings.Contains(result, "%D0%A4") {
		t.Fatalf("contentDisposition() has no UTF-8 filename: %q", result)
	}
}
