package telegramnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	db          *pgxpool.Pool
	botToken    string
	botUsername string
	interval    time.Duration
	client      *http.Client
	apiBase     string
	logger      *slog.Logger
}

type payload struct {
	RoomName       string    `json:"room_name"`
	RoomSlug       string    `json:"room_slug"`
	Actor          string    `json:"actor_name"`
	Filename       string    `json:"filename"`
	Count          int       `json:"count"`
	HoursRemaining int       `json:"hours_remaining"`
	ExpiresAt      time.Time `json:"expires_at"`
	ReportID       string    `json:"report_id"`
	PublicID       string    `json:"public_id"`
	Category       string    `json:"category"`
	Description    string    `json:"description"`
}

func New(db *pgxpool.Pool, botToken, botUsername string, interval time.Duration, logger *slog.Logger) *Worker {
	return &Worker{db: db, botToken: botToken, botUsername: botUsername, interval: interval, client: &http.Client{Timeout: 10 * time.Second}, apiBase: "https://api.telegram.org", logger: logger}
}

func (worker *Worker) Run(ctx context.Context) error {
	if worker.botToken == "" {
		worker.logger.Info("Telegram notifications disabled: bot token is not configured")
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		processed, err := worker.processAdminOne(ctx)
		if err != nil {
			worker.logger.Error("process Telegram admin notification", "error", err)
		}
		if !processed {
			processed, err = worker.processOne(ctx)
			if err != nil {
				worker.logger.Error("process Telegram notification", "error", err)
			}
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *Worker) processAdminOne(ctx context.Context) (bool, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id uuid.UUID
	var telegramUserID int64
	var eventType string
	var rawPayload []byte
	var attempts int
	err = tx.QueryRow(ctx, `
		SELECT id, telegram_user_id, event_type, payload, attempts
		FROM admin_notification_outbox
		WHERE sent_at IS NULL AND failed_at IS NULL AND available_at <= now()
		ORDER BY available_at, created_at
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&id, &telegramUserID, &eventType, &rawPayload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	var data payload
	if err := json.Unmarshal(rawPayload, &data); err != nil {
		_, _ = tx.Exec(ctx, `UPDATE admin_notification_outbox SET failed_at = now(), last_error = 'invalid payload' WHERE id = $1`, id)
		return true, tx.Commit(ctx)
	}
	if eventType != "problem_report_created" {
		_, _ = tx.Exec(ctx, `UPDATE admin_notification_outbox SET failed_at = now(), last_error = 'unsupported event type' WHERE id = $1`, id)
		return true, tx.Commit(ctx)
	}
	if err := worker.sendProblemReport(ctx, telegramUserID, data); err != nil {
		attempts++
		if attempts >= 8 || isPermanent(err) {
			_, err = tx.Exec(ctx, `UPDATE admin_notification_outbox SET attempts = $2, failed_at = now(), last_error = $3 WHERE id = $1`, id, attempts, err.Error())
		} else {
			delay := time.Duration(1<<min(attempts, 8)) * time.Second
			_, err = tx.Exec(ctx, `UPDATE admin_notification_outbox SET attempts = $2, available_at = now() + $3::interval, last_error = $4 WHERE id = $1`, id, attempts, delay.String(), err.Error())
		}
		if err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE admin_notification_outbox SET sent_at = now(), last_error = NULL WHERE id = $1`, id); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func (worker *Worker) processOne(ctx context.Context) (bool, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id uuid.UUID
	var telegramUserID int64
	var eventType string
	var rawPayload []byte
	var attempts int
	err = tx.QueryRow(ctx, `
		SELECT notification.id, notification.telegram_user_id, notification.event_type, notification.payload, notification.attempts
		FROM telegram_notification_outbox notification
		JOIN rooms room ON room.id = notification.room_id
		WHERE notification.sent_at IS NULL AND notification.failed_at IS NULL
		  AND notification.available_at <= now()
		  AND room.status = 'active' AND room.expires_at > now()
		  AND (
			notification.event_type <> 'room_expiry'
			OR COALESCE((notification.payload->>'hours_remaining')::int, 1) <= 1
			OR room.expires_at > now() + interval '1 hour'
		  )
		ORDER BY notification.available_at, notification.created_at
		LIMIT 1
		FOR UPDATE OF notification SKIP LOCKED
	`).Scan(&id, &telegramUserID, &eventType, &rawPayload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	var data payload
	if err := json.Unmarshal(rawPayload, &data); err != nil {
		_, _ = tx.Exec(ctx, `UPDATE telegram_notification_outbox SET failed_at = now(), last_error = 'invalid payload' WHERE id = $1`, id)
		return true, tx.Commit(ctx)
	}
	if err := worker.send(ctx, telegramUserID, eventType, data); err != nil {
		attempts++
		if attempts >= 8 || isPermanent(err) {
			_, err = tx.Exec(ctx, `UPDATE telegram_notification_outbox SET attempts = $2, failed_at = now(), last_error = $3 WHERE id = $1`, id, attempts, err.Error())
		} else {
			delay := time.Duration(1<<min(attempts, 8)) * time.Second
			_, err = tx.Exec(ctx, `UPDATE telegram_notification_outbox SET attempts = $2, available_at = now() + $3::interval, last_error = $4 WHERE id = $1`, id, attempts, delay.String(), err.Error())
		}
		if err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE telegram_notification_outbox SET sent_at = now(), last_error = NULL WHERE id = $1`, id); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

type permanentError struct{ error }

func isPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

func (worker *Worker) send(ctx context.Context, chatID int64, eventType string, data payload) error {
	text := ""
	switch eventType {
	case "member_joined":
		text = fmt.Sprintf("%s приєднався(-лася) до кімнати «%s».", data.Actor, data.RoomName)
	case "media_uploaded":
		if data.Count > 1 {
			text = fmt.Sprintf("%s додав(ла) %d нових файлів у «%s».", data.Actor, data.Count, data.RoomName)
		} else {
			text = fmt.Sprintf("%s додав(ла) файл «%s» у «%s».", data.Actor, data.Filename, data.RoomName)
		}
	case "room_expiry":
		if data.HoursRemaining <= 1 {
			text = fmt.Sprintf("Кімната «%s» буде видалена менш ніж за годину. Після цього файли відновити неможливо.", data.RoomName)
		} else {
			text = fmt.Sprintf("До видалення кімнати «%s» залишилося менше 6 годин. Збережіть потрібні оригінали.", data.RoomName)
		}
	default:
		return permanentError{fmt.Errorf("unsupported event type %q", eventType)}
	}
	requestBody := map[string]any{
		"chat_id": chatID,
		"text":    text,
		"reply_markup": map[string]any{"inline_keyboard": [][]map[string]string{{{
			"text": "Відкрити кімнату",
			"url":  "https://t.me/" + worker.botUsername + "?startapp=room_" + data.RoomSlug,
		}}}},
	}
	return worker.sendMessage(ctx, requestBody)
}

func (worker *Worker) sendProblemReport(ctx context.Context, chatID int64, data payload) error {
	category := map[string]string{"upload": "Завантаження", "download": "Збереження", "room": "Кімната або запрошення", "telegram": "Telegram", "other": "Інше"}[data.Category]
	if category == "" {
		category = "Інше"
	}
	text := fmt.Sprintf("Нове звернення %s\nКатегорія: %s\n\n%s", data.PublicID, category, data.Description)
	return worker.sendMessage(ctx, map[string]any{
		"chat_id": chatID,
		"text":    text,
		"reply_markup": map[string]any{"inline_keyboard": [][]map[string]string{{{
			"text": "Відкрити звернення",
			"url":  "https://t.me/" + worker.botUsername + "?startapp=admin_report_" + data.ReportID,
		}}}},
	})
}

func (worker *Worker) sendMessage(ctx context.Context, requestBody map[string]any) error {
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, worker.apiBase+"/bot"+worker.botToken+"/sendMessage", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := worker.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	result := fmt.Errorf("Telegram API returned status %s", strconv.Itoa(response.StatusCode))
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusForbidden {
		return permanentError{result}
	}
	return result
}
