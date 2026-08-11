-- +goose Up
CREATE TABLE room_realtime_events (
    id bigserial PRIMARY KEY,
    room_id uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    event_type varchar(40) NOT NULL CHECK (event_type IN (
        'media_ready',
        'media_updated',
        'media_deleted',
        'favorite_changed',
        'room_updated',
        'members_changed'
    )),
    entity_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX room_realtime_events_room_id_idx
    ON room_realtime_events (room_id, id);

-- +goose StatementBegin
CREATE FUNCTION emit_room_realtime_event(
    target_room_id uuid,
    target_event_type varchar,
    target_entity_id uuid DEFAULT NULL
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    event_id bigint;
BEGIN
    INSERT INTO room_realtime_events (room_id, event_type, entity_id)
    VALUES (target_room_id, target_event_type, target_entity_id)
    RETURNING id INTO event_id;

    PERFORM pg_notify('room_realtime', target_room_id::text || ':' || event_id::text);
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION media_realtime_event_trigger() RETURNS trigger
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

CREATE TRIGGER media_realtime_event
AFTER UPDATE ON media
FOR EACH ROW EXECUTE FUNCTION media_realtime_event_trigger();

-- +goose StatementBegin
CREATE FUNCTION favorite_realtime_event_trigger() RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_media_id uuid;
    target_room_id uuid;
BEGIN
    target_media_id := COALESCE(NEW.media_id, OLD.media_id);
    SELECT room_id INTO target_room_id FROM media WHERE id = target_media_id;
    IF target_room_id IS NOT NULL THEN
        PERFORM emit_room_realtime_event(target_room_id, 'favorite_changed', target_media_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER favorite_realtime_event
AFTER INSERT OR DELETE ON media_favorites
FOR EACH ROW EXECUTE FUNCTION favorite_realtime_event_trigger();

-- +goose StatementBegin
CREATE FUNCTION room_realtime_event_trigger() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        OLD.name, OLD.owner_identity_id, OLD.access_mode, OLD.status,
        OLD.accepting_uploads, OLD.accepting_members, OLD.expires_at, OLD.cover_media_id
    ) IS DISTINCT FROM ROW(
        NEW.name, NEW.owner_identity_id, NEW.access_mode, NEW.status,
        NEW.accepting_uploads, NEW.accepting_members, NEW.expires_at, NEW.cover_media_id
    ) THEN
        PERFORM emit_room_realtime_event(NEW.id, 'room_updated', NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER room_realtime_event
AFTER UPDATE ON rooms
FOR EACH ROW EXECUTE FUNCTION room_realtime_event_trigger();

-- +goose StatementBegin
CREATE FUNCTION member_realtime_event_trigger() RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_room_id uuid;
    target_identity_id uuid;
BEGIN
    target_room_id := COALESCE(NEW.room_id, OLD.room_id);
    target_identity_id := COALESCE(NEW.identity_id, OLD.identity_id);
    PERFORM emit_room_realtime_event(target_room_id, 'members_changed', target_identity_id);
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER member_realtime_event
AFTER INSERT OR UPDATE OR DELETE ON room_members
FOR EACH ROW EXECUTE FUNCTION member_realtime_event_trigger();

-- +goose Down
DROP TRIGGER IF EXISTS member_realtime_event ON room_members;
DROP FUNCTION IF EXISTS member_realtime_event_trigger();
DROP TRIGGER IF EXISTS room_realtime_event ON rooms;
DROP FUNCTION IF EXISTS room_realtime_event_trigger();
DROP TRIGGER IF EXISTS favorite_realtime_event ON media_favorites;
DROP FUNCTION IF EXISTS favorite_realtime_event_trigger();
DROP TRIGGER IF EXISTS media_realtime_event ON media;
DROP FUNCTION IF EXISTS media_realtime_event_trigger();
DROP FUNCTION IF EXISTS emit_room_realtime_event(uuid, varchar, uuid);
DROP TABLE IF EXISTS room_realtime_events;
