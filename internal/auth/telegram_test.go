package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedTelegramData(values url.Values, token string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte(token))
	signature := hmac.New(sha256.New, secret.Sum(nil))
	_, _ = signature.Write([]byte(strings.Join(parts, "\n")))
	values.Set("hash", hex.EncodeToString(signature.Sum(nil)))
	return values.Encode()
}

func TestValidateTelegramInitData(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	token := "123456:test-secret"
	raw := signedTelegramData(url.Values{
		"auth_date": {strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)},
		"query_id":  {"AAHdF6IQAAAAAN0XohDhrOrc"},
		"user":      {`{"id":123456789,"first_name":"Олена","last_name":"Коваль","username":"olena"}`},
	}, token)

	user, err := ValidateTelegramInitData(raw, token, 10*time.Minute, now)
	if err != nil {
		t.Fatalf("validate init data: %v", err)
	}
	if user.ID != 123456789 || user.DisplayName() != "Олена Коваль" {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestValidateTelegramInitDataRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	token := "123456:test-secret"
	values := url.Values{
		"auth_date": {strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)},
		"user":      {`{"id":7,"first_name":"Mia"}`},
	}
	raw := signedTelegramData(values, token)
	tampered := strings.Replace(raw, "Mia", "Max", 1)
	if _, err := ValidateTelegramInitData(tampered, token, 10*time.Minute, now); !errors.Is(err, ErrInvalidTelegramData) {
		t.Fatalf("expected invalid data, got %v", err)
	}

	expired := signedTelegramData(url.Values{
		"auth_date": {strconv.FormatInt(now.Add(-11*time.Minute).Unix(), 10)},
		"user":      {`{"id":7,"first_name":"Mia"}`},
	}, token)
	if _, err := ValidateTelegramInitData(expired, token, 10*time.Minute, now); !errors.Is(err, ErrExpiredTelegramData) {
		t.Fatalf("expected expired data, got %v", err)
	}
}

func TestValidateTelegramInitDataRejectsDuplicateFields(t *testing.T) {
	_, err := ValidateTelegramInitData("auth_date=1&auth_date=2&user=%7B%7D&hash=00", "token", time.Minute, time.Now())
	if !errors.Is(err, ErrInvalidTelegramData) {
		t.Fatalf("expected invalid data, got %v", err)
	}
}
