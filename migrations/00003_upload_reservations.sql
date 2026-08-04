-- +goose Up
ALTER TABLE rooms
    ADD COLUMN reserved_files integer NOT NULL DEFAULT 0 CHECK (reserved_files >= 0),
    ADD COLUMN reserved_storage_bytes bigint NOT NULL DEFAULT 0 CHECK (reserved_storage_bytes >= 0),
    ADD CONSTRAINT rooms_total_files_limit_check
        CHECK (used_files + reserved_files <= max_files),
    ADD CONSTRAINT rooms_total_storage_limit_check
        CHECK (used_storage_bytes + reserved_storage_bytes <= max_storage_bytes);

ALTER TABLE upload_sessions
    ADD COLUMN idempotency_key uuid,
    ADD COLUMN request_hash bytea;

ALTER TABLE upload_sessions
    ADD CONSTRAINT upload_sessions_request_hash_length_check
        CHECK (request_hash IS NULL OR octet_length(request_hash) = 32);

CREATE UNIQUE INDEX upload_sessions_identity_idempotency_uidx
    ON upload_sessions (identity_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS upload_sessions_identity_idempotency_uidx;
ALTER TABLE upload_sessions
    DROP CONSTRAINT IF EXISTS upload_sessions_request_hash_length_check,
    DROP COLUMN IF EXISTS request_hash,
    DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE rooms
    DROP CONSTRAINT IF EXISTS rooms_total_storage_limit_check,
    DROP CONSTRAINT IF EXISTS rooms_total_files_limit_check,
    DROP COLUMN IF EXISTS reserved_storage_bytes,
    DROP COLUMN IF EXISTS reserved_files;
