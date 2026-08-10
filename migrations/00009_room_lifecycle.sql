-- +goose Up
ALTER TABLE rooms
    ADD COLUMN accepting_members boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE rooms
    DROP COLUMN IF EXISTS accepting_members;
