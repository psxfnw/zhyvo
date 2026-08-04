-- +goose Up
ALTER TABLE media
    ADD COLUMN thumbnail_status varchar(20) NOT NULL DEFAULT 'pending'
        CHECK (thumbnail_status IN ('pending', 'processing', 'ready', 'failed')),
    ADD COLUMN thumbnail_attempts integer NOT NULL DEFAULT 0 CHECK (thumbnail_attempts >= 0),
    ADD COLUMN thumbnail_error text,
    ADD COLUMN thumbnail_updated_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX media_thumbnail_queue_idx
    ON media (thumbnail_status, thumbnail_updated_at)
    WHERE status = 'ready' AND thumbnail_key IS NULL;

-- +goose Down
DROP INDEX IF EXISTS media_thumbnail_queue_idx;
ALTER TABLE media
    DROP COLUMN IF EXISTS thumbnail_updated_at,
    DROP COLUMN IF EXISTS thumbnail_error,
    DROP COLUMN IF EXISTS thumbnail_attempts,
    DROP COLUMN IF EXISTS thumbnail_status;
