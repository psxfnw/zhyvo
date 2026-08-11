-- +goose Up
ALTER TABLE telegram_notification_outbox
    DROP CONSTRAINT telegram_notification_outbox_event_type_check;

ALTER TABLE telegram_notification_outbox
    ADD CONSTRAINT telegram_notification_outbox_event_type_check
    CHECK (event_type IN ('member_joined', 'media_uploaded', 'room_expiry'));

-- +goose Down
DELETE FROM telegram_notification_outbox WHERE event_type = 'room_expiry';

ALTER TABLE telegram_notification_outbox
    DROP CONSTRAINT telegram_notification_outbox_event_type_check;

ALTER TABLE telegram_notification_outbox
    ADD CONSTRAINT telegram_notification_outbox_event_type_check
    CHECK (event_type IN ('member_joined', 'media_uploaded'));
