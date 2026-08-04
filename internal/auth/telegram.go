package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidTelegramData = errors.New("invalid Telegram init data")
	ErrExpiredTelegramData = errors.New("expired Telegram init data")
)

type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Language  string `json:"language_code"`
	PhotoURL  string `json:"photo_url"`
}

func (user TelegramUser) DisplayName() string {
	name := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if name == "" {
		name = strings.TrimSpace(user.Username)
	}
	if name == "" {
		name = "Telegram user"
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}

func ValidateTelegramInitData(raw, botToken string, maxAge time.Duration, now time.Time) (TelegramUser, error) {
	if raw == "" || botToken == "" || len(raw) > 16<<10 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	for _, entries := range values {
		if len(entries) != 1 {
			return TelegramUser{}, ErrInvalidTelegramData
		}
	}

	providedHash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(providedHash) != sha256.Size {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	keys := make([]string, 0, len(values)-1)
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	signatureMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = signatureMAC.Write([]byte(strings.Join(parts, "\n")))
	if !hmac.Equal(providedHash, signatureMAC.Sum(nil)) {
		return TelegramUser{}, ErrInvalidTelegramData
	}

	authUnix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || authUnix <= 0 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	authTime := time.Unix(authUnix, 0)
	if authTime.After(now.Add(30*time.Second)) || now.Sub(authTime) > maxAge {
		return TelegramUser{}, ErrExpiredTelegramData
	}

	var user TelegramUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return TelegramUser{}, fmt.Errorf("%w: user is missing", ErrInvalidTelegramData)
	}
	return user, nil
}
