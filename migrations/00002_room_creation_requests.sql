-- +goose Up
CREATE TABLE room_creation_requests (
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    room_id uuid REFERENCES rooms(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now() + interval '24 hours',
    PRIMARY KEY (identity_id, idempotency_key),
    CHECK (expires_at > created_at)
);

CREATE INDEX room_creation_requests_expiry_idx ON room_creation_requests (expires_at);

-- +goose Down
DROP TABLE IF EXISTS room_creation_requests;
