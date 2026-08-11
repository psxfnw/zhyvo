-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION member_realtime_event_trigger() RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_room_id uuid;
    target_identity_id uuid;
BEGIN
    target_room_id := COALESCE(NEW.room_id, OLD.room_id);
    target_identity_id := COALESCE(NEW.identity_id, OLD.identity_id);
    IF EXISTS (SELECT 1 FROM rooms WHERE id = target_room_id) THEN
        PERFORM emit_room_realtime_event(target_room_id, 'members_changed', target_identity_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION member_realtime_event_trigger() RETURNS trigger
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
