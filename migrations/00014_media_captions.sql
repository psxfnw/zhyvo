-- +goose Up
ALTER TABLE media
    ADD COLUMN caption text CHECK (caption IS NULL OR char_length(caption) <= 300),
    ADD COLUMN caption_updated_at timestamptz;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION media_realtime_event_trigger() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IS DISTINCT FROM NEW.status AND NEW.status = 'ready' THEN
        PERFORM emit_room_realtime_event(NEW.room_id, 'media_ready', NEW.id);
    ELSIF OLD.status IS DISTINCT FROM NEW.status AND NEW.status = 'deleted' THEN
        PERFORM emit_room_realtime_event(NEW.room_id, 'media_deleted', NEW.id);
    ELSIF NEW.status = 'ready' AND (
        OLD.thumbnail_status IS DISTINCT FROM NEW.thumbnail_status
        OR OLD.width IS DISTINCT FROM NEW.width
        OR OLD.height IS DISTINCT FROM NEW.height
        OR OLD.duration_ms IS DISTINCT FROM NEW.duration_ms
        OR OLD.caption IS DISTINCT FROM NEW.caption
    ) THEN
        PERFORM emit_room_realtime_event(NEW.room_id, 'media_updated', NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION media_realtime_event_trigger() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IS DISTINCT FROM NEW.status AND NEW.status = 'ready' THEN
        PERFORM emit_room_realtime_event(NEW.room_id, 'media_ready', NEW.id);
    ELSIF OLD.status IS DISTINCT FROM NEW.status AND NEW.status = 'deleted' THEN
        PERFORM emit_room_realtime_event(NEW.room_id, 'media_deleted', NEW.id);
    ELSIF NEW.status = 'ready' AND (
        OLD.thumbnail_status IS DISTINCT FROM NEW.thumbnail_status
        OR OLD.width IS DISTINCT FROM NEW.width
        OR OLD.height IS DISTINCT FROM NEW.height
        OR OLD.duration_ms IS DISTINCT FROM NEW.duration_ms
    ) THEN
        PERFORM emit_room_realtime_event(NEW.room_id, 'media_updated', NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE media
    DROP COLUMN IF EXISTS caption_updated_at,
    DROP COLUMN IF EXISTS caption;
