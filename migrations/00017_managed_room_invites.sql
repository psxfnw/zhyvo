-- +goose Up
ALTER TABLE room_members
    ADD COLUMN can_upload boolean NOT NULL DEFAULT true;

ALTER TABLE rooms
    ADD COLUMN legacy_invites_enabled boolean NOT NULL DEFAULT true;

CREATE TABLE room_invites (
    token varchar(43) PRIMARY KEY,
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    permission varchar(20) NOT NULL CHECK (permission IN ('contributor', 'viewer')),
    created_by_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    last_used_at timestamptz,
    join_count integer NOT NULL DEFAULT 0 CHECK (join_count >= 0)
);

CREATE INDEX room_invites_room_active_idx
    ON room_invites (room_id, created_at DESC)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS room_invites;
ALTER TABLE rooms DROP COLUMN IF EXISTS legacy_invites_enabled;
ALTER TABLE room_members DROP COLUMN IF EXISTS can_upload;
