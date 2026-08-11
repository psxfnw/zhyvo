package room

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Recap struct {
	MediaCount       int       `json:"media_count"`
	ImageCount       int       `json:"image_count"`
	VideoCount       int       `json:"video_count"`
	MemberCount      int       `json:"member_count"`
	ContributorCount int       `json:"contributor_count"`
	FavoriteCount    int       `json:"favorite_count"`
	TotalBytes       int64     `json:"total_bytes"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func (s *Service) Recap(ctx context.Context, identityID uuid.UUID, slug string) (Recap, error) {
	currentRoom, err := s.Get(ctx, identityID, slug)
	if err != nil {
		return Recap{}, err
	}
	var result Recap
	err = s.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE media.status = 'ready')::integer,
			count(*) FILTER (WHERE media.status = 'ready' AND media.media_type = 'image')::integer,
			count(*) FILTER (WHERE media.status = 'ready' AND media.media_type = 'video')::integer,
			(SELECT count(*)::integer FROM room_members WHERE room_id = $1),
			count(DISTINCT media.uploader_identity_id) FILTER (WHERE media.status = 'ready')::integer,
			(SELECT count(*)::integer FROM media_favorites favorite JOIN media favorite_media ON favorite_media.id = favorite.media_id WHERE favorite_media.room_id = $1 AND favorite_media.status = 'ready'),
			COALESCE(sum(media.size_bytes) FILTER (WHERE media.status = 'ready'), 0)::bigint,
			$2::timestamptz,
			$3::timestamptz
		FROM media
		WHERE media.room_id = $1
	`, currentRoom.ID, currentRoom.CreatedAt, currentRoom.ExpiresAt).Scan(
		&result.MediaCount, &result.ImageCount, &result.VideoCount, &result.MemberCount,
		&result.ContributorCount, &result.FavoriteCount, &result.TotalBytes,
		&result.CreatedAt, &result.ExpiresAt,
	)
	if err != nil {
		return Recap{}, fmt.Errorf("build room recap: %w", err)
	}
	return result, nil
}
