package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidAccessToken = errors.New("invalid access token")

type Principal struct {
	IdentityID uuid.UUID
	SessionID  uuid.UUID
	Kind       string
}

type TokenManager struct {
	secret []byte
	ttl    time.Duration
	issuer string
	now    func() time.Time
}

type accessClaims struct {
	SessionID string `json:"sid"`
	Kind      string `json:"kind"`
	jwt.RegisteredClaims
}

func NewTokenManager(secret, issuer string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl, issuer: issuer, now: time.Now}
}

func (m *TokenManager) Issue(principal Principal) (string, time.Time, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.ttl)
	claims := accessClaims{
		SessionID: principal.SessionID.String(),
		Kind:      principal.Kind,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   principal.IdentityID.String(),
			Audience:  jwt.ClaimStrings{"photodrop-api"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, expiresAt, nil
}

func (m *TokenManager) Parse(raw string) (Principal, error) {
	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidAccessToken
			}
			return m.secret, nil
		},
		jwt.WithAudience("photodrop-api"),
		jwt.WithIssuer(m.issuer),
		jwt.WithLeeway(5*time.Second),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil || !token.Valid {
		return Principal{}, ErrInvalidAccessToken
	}

	identityID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return Principal{}, ErrInvalidAccessToken
	}
	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil || claims.Kind == "" {
		return Principal{}, ErrInvalidAccessToken
	}

	return Principal{IdentityID: identityID, SessionID: sessionID, Kind: claims.Kind}, nil
}
