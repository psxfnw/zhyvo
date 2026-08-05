package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestTelegramOIDCExchangeAndNonceValidation(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"iss": telegramIssuer, "sub": "987654321", "aud": "123456",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"id": "987654321", "name": "Zhyvo Tester", "preferred_username": "zhyvo_test", "nonce": "expected-nonce",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	rawToken, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	exponent := big.NewInt(int64(privateKey.PublicKey.E)).Bytes()
	jwks, _ := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kid": "test-key", "kty": "RSA",
		"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(exponent),
	}}})
	oidc := NewTelegramOIDC("123456", "client-secret")
	oidc.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := string(jwks)
		if request.URL.String() == telegramTokenURL {
			body = `{"id_token":"` + rawToken + `"}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})}

	user, err := oidc.Exchange(context.Background(), "authorization-code", strings.Repeat("v", 43), "https://example.com/auth/telegram/callback", "expected-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 987654321 || user.DisplayName() != "Zhyvo Tester" {
		t.Fatalf("unexpected Telegram user: %+v", user)
	}
	if _, err := oidc.VerifyIDToken(context.Background(), rawToken, "wrong-nonce"); err == nil {
		t.Fatal("expected nonce mismatch to be rejected")
	}
}
