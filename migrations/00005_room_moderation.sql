-- +goose Up
CREATE TABLE room_bans (
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    banned_by_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, identity_id)
);

CREATE INDEX room_bans_identity_room_idx ON room_bans (identity_id, room_id);

CREATE TABLE room_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    event_type varchar(40) NOT NULL CHECK (event_type IN (
        'room_created',
        'member_joined',
        'member_removed',
        'member_unblocked',
        'ownership_transferred',
        'room_updated'
    )),
    actor_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    actor_display_name varchar(80) NOT NULL,
    subject_identity_id uuid REFERENCES identities(id) ON DELETE SET NULL,
    subject_display_name varchar(80),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX room_events_room_created_idx ON room_events (room_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS room_events;
DROP TABLE IF EXISTS room_bans;
