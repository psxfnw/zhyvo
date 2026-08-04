-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind varchar(20) NOT NULL CHECK (kind IN ('anonymous', 'telegram', 'account')),
    display_name varchar(80) NOT NULL CHECK (char_length(trim(display_name)) BETWEEN 1 AND 80),
    telegram_user_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX identities_telegram_user_id_uidx
    ON identities (telegram_user_id)
    WHERE telegram_user_id IS NOT NULL;

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL,
    client_type varchar(20) NOT NULL CHECK (client_type IN ('web', 'telegram', 'ios', 'android')),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX sessions_token_hash_uidx ON sessions (token_hash);
CREATE INDEX sessions_identity_id_idx ON sessions (identity_id);
CREATE INDEX sessions_active_expiry_idx ON sessions (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE rooms (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug varchar(12) NOT NULL,
    name varchar(120) NOT NULL CHECK (char_length(trim(name)) BETWEEN 1 AND 120),
    owner_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    access_mode varchar(20) NOT NULL CHECK (access_mode IN ('public', 'pin', 'password')),
    access_secret_hash text,
    status varchar(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleting')),
    accepting_uploads boolean NOT NULL DEFAULT true,
    max_files integer NOT NULL DEFAULT 1000 CHECK (max_files > 0),
    max_storage_bytes bigint NOT NULL DEFAULT 21474836480 CHECK (max_storage_bytes > 0),
    used_files integer NOT NULL DEFAULT 0 CHECK (used_files >= 0),
    used_storage_bytes bigint NOT NULL DEFAULT 0 CHECK (used_storage_bytes >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (slug = upper(slug)),
    CHECK (
        (access_mode = 'public' AND access_secret_hash IS NULL)
        OR (access_mode IN ('pin', 'password') AND access_secret_hash IS NOT NULL)
    ),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '3 days'),
    CHECK (used_files <= max_files),
    CHECK (used_storage_bytes <= max_storage_bytes)
);

CREATE UNIQUE INDEX rooms_slug_uidx ON rooms (slug);
CREATE INDEX rooms_cleanup_idx ON rooms (status, expires_at);
CREATE INDEX rooms_owner_identity_id_idx ON rooms (owner_identity_id);

CREATE TABLE room_members (
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    role varchar(20) NOT NULL CHECK (role IN ('owner', 'member')),
    joined_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, identity_id)
);

CREATE INDEX room_members_identity_room_idx ON room_members (identity_id, room_id);
CREATE UNIQUE INDEX room_single_owner_uidx ON room_members (room_id) WHERE role = 'owner';

CREATE TABLE media (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    uploader_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    status varchar(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'deleted')),
    media_type varchar(20) NOT NULL CHECK (media_type IN ('image', 'video')),
    original_filename varchar(255) NOT NULL,
    mime_type varchar(120) NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    storage_key text NOT NULL,
    thumbnail_key text,
    width integer CHECK (width IS NULL OR width > 0),
    height integer CHECK (height IS NULL OR height > 0),
    duration_ms bigint CHECK (duration_ms IS NULL OR duration_ms >= 0),
    checksum varchar(128),
    captured_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    ready_at timestamptz,
    deleted_at timestamptz
);

CREATE UNIQUE INDEX media_storage_key_uidx ON media (storage_key);
CREATE INDEX media_gallery_idx ON media (room_id, created_at DESC, id DESC)
    WHERE status = 'ready';
CREATE INDEX media_uploader_idx ON media (uploader_identity_id);

CREATE TABLE upload_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    media_id uuid NOT NULL UNIQUE REFERENCES media(id) ON DELETE CASCADE,
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    storage_upload_id text,
    upload_type varchar(20) NOT NULL CHECK (upload_type IN ('single', 'multipart')),
    status varchar(20) NOT NULL DEFAULT 'initiated' CHECK (status IN ('initiated', 'uploading', 'completed', 'aborted', 'expired')),
    part_size_bytes bigint CHECK (part_size_bytes IS NULL OR part_size_bytes > 0),
    parts_count integer CHECK (parts_count IS NULL OR parts_count > 0),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK (
        (upload_type = 'single' AND storage_upload_id IS NULL)
        OR (upload_type = 'multipart' AND storage_upload_id IS NOT NULL)
    )
);

CREATE INDEX upload_sessions_cleanup_idx ON upload_sessions (status, expires_at);
CREATE INDEX upload_sessions_identity_id_idx ON upload_sessions (identity_id);

-- +goose Down
DROP TABLE IF EXISTS upload_sessions;
DROP TABLE IF EXISTS media;
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS identities;
DROP EXTENSION IF EXISTS pgcrypto;
