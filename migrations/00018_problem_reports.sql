-- +goose Up
CREATE TABLE problem_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    public_id varchar(16) NOT NULL UNIQUE,
    reporter_identity_id uuid REFERENCES identities(id) ON DELETE SET NULL,
    category varchar(32) NOT NULL CHECK (category IN ('upload', 'download', 'room', 'telegram', 'other')),
    description text NOT NULL CHECK (char_length(description) BETWEEN 10 AND 2000),
    contact varchar(160),
    technical_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(24) NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'in_progress', 'resolved', 'closed')),
    admin_note text CHECK (admin_note IS NULL OR char_length(admin_note) <= 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

CREATE INDEX problem_reports_status_created_idx
    ON problem_reports (status, created_at DESC);

CREATE TABLE admin_notification_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    problem_report_id uuid NOT NULL REFERENCES problem_reports(id) ON DELETE CASCADE,
    telegram_user_id bigint NOT NULL,
    event_type varchar(40) NOT NULL CHECK (event_type IN ('problem_report_created')),
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    failed_at timestamptz,
    last_error text,
    UNIQUE (problem_report_id, telegram_user_id, event_type)
);

CREATE INDEX admin_notification_outbox_pending_idx
    ON admin_notification_outbox (available_at, created_at)
    WHERE sent_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS admin_notification_outbox;
DROP TABLE IF EXISTS problem_reports;
