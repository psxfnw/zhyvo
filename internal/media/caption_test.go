package media

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCaption(t *testing.T) {
	got, err := normalizeCaption("  Перший танець  \n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Перший танець" {
		t.Fatalf("caption = %q", got)
	}
}

func TestNormalizeCaptionCountsUnicodeCharacters(t *testing.T) {
	if _, err := normalizeCaption(strings.Repeat("ї", 300)); err != nil {
		t.Fatalf("300 Unicode characters rejected: %v", err)
	}
	if _, err := normalizeCaption(strings.Repeat("ї", 301)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("301 Unicode characters error = %v, want ErrInvalidInput", err)
	}
}
