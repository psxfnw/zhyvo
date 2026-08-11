-- +goose Up
CREATE UNIQUE INDEX media_room_checksum_active_uidx
    ON media (room_id, checksum)
    WHERE checksum IS NOT NULL
      AND status IN ('pending', 'processing', 'ready');

-- +goose Down
DROP INDEX IF EXISTS media_room_checksum_active_uidx;
