-- +goose Up
ALTER TABLE room_notification_preferences
    ADD COLUMN new_media_enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN expiry_enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN member_joined_enabled boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE room_notification_preferences
    DROP COLUMN IF EXISTS member_joined_enabled,
    DROP COLUMN IF EXISTS expiry_enabled,
    DROP COLUMN IF EXISTS new_media_enabled;
