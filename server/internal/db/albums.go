package db

import (
	"context"
	"fmt"
)

// AlbumOfTrack devuelve el álbum de una pista y si ya tiene carátula.
func AlbumOfTrack(ctx context.Context, q Querier, trackID int64) (albumID int64, hasCover bool, err error) {
	err = q.QueryRow(ctx, `SELECT al.id, al.cover_ext IS NOT NULL
		FROM tracks t JOIN albums al ON al.id = t.album_id
		WHERE t.id = $1`, trackID).Scan(&albumID, &hasCover)
	if err != nil {
		return 0, false, fmt.Errorf("buscar el álbum de la pista %d: %w", trackID, err)
	}
	return albumID, hasCover, nil
}

// SetAlbumCover guarda la extensión de la carátula de un álbum. Solo si el álbum
// no tenía: la primera pista con imagen fija la carátula (ADR-015), y una
// segunda no puede pisarla aunque quien llama se equivoque.
func SetAlbumCover(ctx context.Context, q Querier, albumID int64, ext string) error {
	tag, err := q.Exec(ctx, "UPDATE albums SET cover_ext = $2 WHERE id = $1 AND cover_ext IS NULL", albumID, ext)
	if err != nil {
		return fmt.Errorf("guardar la carátula del álbum %d: %w", albumID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("guardar la carátula del álbum %d: el álbum no existe o ya tenía carátula", albumID)
	}
	return nil
}
