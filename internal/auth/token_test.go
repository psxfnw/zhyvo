package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTokenManagerIssueAndParse(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	manager := NewTokenManager("test_secret_that_is_longer_than_32_characters", "photodrop", 15*time.Minute)
	manager.now = func() time.Time { return fixedNow }

	want := Principal{
		IdentityID: uuid.New(),
		SessionID:  uuid.New(),
		Kind:       "anonymous",
	}
	raw, expiresAt, err := manager.Issue(want)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if wantExpiry := fixedNow.Add(15 * time.Minute); !expiresAt.Equal(wantExpiry) {
		t.Fatalf("Issue() expiry = %v, want %v", expiresAt, wantExpiry)
	}

	got, err := manager.Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got != want {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestTokenManagerRejectsExpiredToken(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	manager := NewTokenManager("test_secret_that_is_longer_than_32_characters", "photodrop", time.Minute)
	manager.now = func() time.Time { return fixedNow }

	raw, _, err := manager.Issue(Principal{IdentityID: uuid.New(), SessionID: uuid.New(), Kind: "anonymous"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	manager.now = func() time.Time { return fixedNow.Add(2 * time.Minute) }

	_, err = manager.Parse(raw)
	if !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidAccessToken", err)
	}
}

func TestTokenManagerRejectsDifferentSecret(t *testing.T) {
	issuer := NewTokenManager("first_secret_that_is_longer_than_32_characters", "photodrop", 15*time.Minute)
	raw, _, err := issuer.Issue(Principal{IdentityID: uuid.New(), SessionID: uuid.New(), Kind: "anonymous"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	parser := NewTokenManager("second_secret_that_is_longer_than_32_chars", "photodrop", 15*time.Minute)
	_, err = parser.Parse(raw)
	if !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidAccessToken", err)
	}
}
