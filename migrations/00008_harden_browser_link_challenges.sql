-- +goose Up
WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY source_identity_id ORDER BY created_at DESC) AS position
    FROM browser_link_challenges
    WHERE status IN ('pending', 'approved')
)
UPDATE browser_link_challenges challenge
SET status = 'denied'
FROM ranked
WHERE challenge.id = ranked.id AND ranked.position > 1;

CREATE UNIQUE INDEX browser_link_challenges_one_active_source_uidx
    ON browser_link_challenges (source_identity_id)
    WHERE status IN ('pending', 'approved');

ALTER TABLE browser_link_challenges
    DROP CONSTRAINT browser_link_challenges_source_identity_id_fkey,
    ALTER COLUMN source_identity_id DROP NOT NULL,
    ADD CONSTRAINT browser_link_challenges_source_identity_id_fkey
        FOREIGN KEY (source_identity_id) REFERENCES identities(id) ON DELETE SET NULL;

-- +goose Down
DELETE FROM browser_link_challenges WHERE source_identity_id IS NULL;

ALTER TABLE browser_link_challenges
    DROP CONSTRAINT browser_link_challenges_source_identity_id_fkey,
    ALTER COLUMN source_identity_id SET NOT NULL,
    ADD CONSTRAINT browser_link_challenges_source_identity_id_fkey
        FOREIGN KEY (source_identity_id) REFERENCES identities(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS browser_link_challenges_one_active_source_uidx;
