-- +goose Up
CREATE TABLE browser_link_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE,
    source_identity_id uuid NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    approved_telegram_user_id bigint,
    approved_display_name varchar(80),
    status varchar(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'consumed', 'denied')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    approved_at timestamptz,
    consumed_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes'),
    CHECK ((status = 'approved' AND approved_telegram_user_id IS NOT NULL) OR status <> 'approved')
);

CREATE INDEX browser_link_challenges_source_idx ON browser_link_challenges (source_identity_id, created_at DESC);
CREATE INDEX browser_link_challenges_cleanup_idx ON browser_link_challenges (expires_at) WHERE status IN ('pending', 'approved');

-- +goose Down
DROP TABLE IF EXISTS browser_link_challenges;
