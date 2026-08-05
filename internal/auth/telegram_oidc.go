package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	telegramIssuer   = "https://oauth.telegram.org"
	telegramTokenURL = "https://oauth.telegram.org/token"
	telegramJWKSURL  = "https://oauth.telegram.org/.well-known/jwks.json"
)

var ErrInvalidTelegramIDToken = errors.New("invalid Telegram ID token")

type TelegramOIDC struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	mu           sync.RWMutex
	keys         map[string]*rsa.PublicKey
	keysExpiry   time.Time
}

type telegramTokenResponse struct {
	IDToken string `json:"id_token"`
}

type telegramOIDCClaims struct {
	jwt.RegisteredClaims
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	PreferredUsername string `json:"preferred_username"`
	Picture           string `json:"picture"`
	Nonce             string `json:"nonce"`
}

func NewTelegramOIDC(clientID, clientSecret string) *TelegramOIDC {
	return &TelegramOIDC{
		clientID: strings.TrimSpace(clientID), clientSecret: strings.TrimSpace(clientSecret),
		httpClient: &http.Client{Timeout: 10 * time.Second}, keys: make(map[string]*rsa.PublicKey),
	}
}

func (oidc *TelegramOIDC) Enabled() bool {
	return oidc != nil && oidc.clientID != "" && oidc.clientSecret != ""
}

func (oidc *TelegramOIDC) ClientID() string {
	if oidc == nil {
		return ""
	}
	return oidc.clientID
}

func (oidc *TelegramOIDC) Exchange(ctx context.Context, code, codeVerifier, redirectURI, expectedNonce string) (TelegramUser, error) {
	if !oidc.Enabled() || code == "" || len(codeVerifier) < 43 || len(codeVerifier) > 128 || expectedNonce == "" {
		return TelegramUser{}, ErrInvalidTelegramIDToken
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {oidc.clientID},
		"code_verifier": {codeVerifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TelegramUser{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(oidc.clientID, oidc.clientSecret)
	response, err := oidc.httpClient.Do(request)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("exchange Telegram authorization code: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return TelegramUser{}, ErrInvalidTelegramIDToken
	}
	var tokens telegramTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&tokens); err != nil || tokens.IDToken == "" {
		return TelegramUser{}, ErrInvalidTelegramIDToken
	}
	return oidc.VerifyIDToken(ctx, tokens.IDToken, expectedNonce)
}

func (oidc *TelegramOIDC) VerifyIDToken(ctx context.Context, raw, expectedNonce string) (TelegramUser, error) {
	claims := &telegramOIDCClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, ErrInvalidTelegramIDToken
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, ErrInvalidTelegramIDToken
		}
		return oidc.key(ctx, kid)
	}, jwt.WithIssuer(telegramIssuer), jwt.WithAudience(oidc.clientID), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(30*time.Second), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !token.Valid || claims.Nonce != expectedNonce {
		return TelegramUser{}, ErrInvalidTelegramIDToken
	}
	id := claims.ID
	if id <= 0 {
		id, err = strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || id <= 0 {
			return TelegramUser{}, ErrInvalidTelegramIDToken
		}
	}
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = strings.TrimSpace(strings.Join([]string{claims.GivenName, claims.FamilyName}, " "))
	}
	return TelegramUser{ID: id, FirstName: name, Username: claims.PreferredUsername, PhotoURL: claims.Picture}, nil
}

func (oidc *TelegramOIDC) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	oidc.mu.RLock()
	key, found := oidc.keys[kid]
	fresh := time.Now().Before(oidc.keysExpiry)
	oidc.mu.RUnlock()
	if found && fresh {
		return key, nil
	}
	if err := oidc.refreshKeys(ctx); err != nil {
		return nil, err
	}
	oidc.mu.RLock()
	defer oidc.mu.RUnlock()
	key, found = oidc.keys[kid]
	if !found {
		return nil, ErrInvalidTelegramIDToken
	}
	return key, nil
}

func (oidc *TelegramOIDC) refreshKeys(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, telegramJWKSURL, nil)
	if err != nil {
		return err
	}
	response, err := oidc.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("fetch Telegram signing keys: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrInvalidTelegramIDToken
	}
	var set struct {
		Keys []struct {
			KID string `json:"kid"`
			KTY string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&set); err != nil {
		return ErrInvalidTelegramIDToken
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, item := range set.Keys {
		if item.KTY != "RSA" || item.KID == "" {
			continue
		}
		nBytes, nErr := base64.RawURLEncoding.DecodeString(item.N)
		eBytes, eErr := base64.RawURLEncoding.DecodeString(item.E)
		if nErr != nil || eErr != nil || len(eBytes) == 0 || len(eBytes) > 4 {
			continue
		}
		exponent := 0
		for _, part := range eBytes {
			exponent = exponent<<8 | int(part)
		}
		if exponent < 3 {
			continue
		}
		keys[item.KID] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: exponent}
	}
	if len(keys) == 0 {
		return ErrInvalidTelegramIDToken
	}
	oidc.mu.Lock()
	oidc.keys = keys
	oidc.keysExpiry = time.Now().Add(time.Hour)
	oidc.mu.Unlock()
	return nil
}
