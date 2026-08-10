-- +goose Up
CREATE TABLE room_notification_preferences (
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    telegram_enabled boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, identity_id)
);

CREATE TABLE telegram_notification_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    telegram_user_id bigint NOT NULL,
    event_type varchar(40) NOT NULL CHECK (event_type IN ('member_joined', 'media_uploaded')),
    payload jsonb NOT NULL,
    dedupe_key text UNIQUE,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    failed_at timestamptz,
    last_error text,
    CHECK (attempts >= 0)
);

CREATE INDEX telegram_notification_outbox_pending_idx
    ON telegram_notification_outbox (available_at, created_at)
    WHERE sent_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS telegram_notification_outbox;
DROP TABLE IF EXISTS room_notification_preferences;
