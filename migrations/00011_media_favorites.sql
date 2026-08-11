-- +goose Up
CREATE TABLE media_favorites (
    media_id uuid NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (media_id, identity_id)
);

CREATE INDEX media_favorites_identity_idx ON media_favorites (identity_id, created_at DESC);

ALTER TABLE rooms
    ADD COLUMN cover_media_id uuid REFERENCES media(id) ON DELETE SET NULL;

CREATE INDEX rooms_cover_media_id_idx ON rooms (cover_media_id)
    WHERE cover_media_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS rooms_cover_media_id_idx;
ALTER TABLE rooms DROP COLUMN IF EXISTS cover_media_id;
DROP TABLE IF EXISTS media_favorites;
