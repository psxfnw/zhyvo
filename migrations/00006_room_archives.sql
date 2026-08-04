-- +goose Up
ALTER TABLE rooms
    ADD COLUMN media_revision bigint NOT NULL DEFAULT 0 CHECK (media_revision >= 0);

CREATE TABLE room_archives (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    requested_by_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    source_revision bigint NOT NULL CHECK (source_revision >= 0),
    status varchar(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'ready', 'failed')),
    object_key text NOT NULL,
    filename varchar(255) NOT NULL,
    total_files integer NOT NULL CHECK (total_files > 0),
    processed_files integer NOT NULL DEFAULT 0 CHECK (processed_files >= 0),
    total_bytes bigint NOT NULL CHECK (total_bytes >= 0),
    processed_bytes bigint NOT NULL DEFAULT 0 CHECK (processed_bytes >= 0),
    size_bytes bigint CHECK (size_bytes >= 0),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (room_id, source_revision)
);

CREATE INDEX room_archives_queue_idx
    ON room_archives (status, updated_at)
    WHERE status IN ('pending', 'processing');

-- +goose Down
DROP TABLE IF EXISTS room_archives;
ALTER TABLE rooms DROP COLUMN IF EXISTS media_revision;
