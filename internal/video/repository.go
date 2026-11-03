package video

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(context.Context, CreateParams) (Video, error)
	FindByID(context.Context, uuid.UUID) (Video, error)
	ListPublic(context.Context, int, *Cursor) ([]Video, error)
	ListByOwner(context.Context, uuid.UUID, int, *Cursor) ([]Video, error)
	Update(context.Context, uuid.UUID, UpdateParams) (Video, error)
	SoftDelete(context.Context, uuid.UUID) error
}

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

const videoColumns = `id, owner_id, title, description, visibility, processing_status, view_count, published_at, created_at, updated_at`

func (r *PostgresRepository) Create(ctx context.Context, params CreateParams) (Video, error) {
	row := r.database.QueryRow(ctx, `
		INSERT INTO videos (owner_id, title, description, visibility)
		VALUES ($1, $2, $3, $4)
		RETURNING `+videoColumns,
		params.OwnerID, params.Title, params.Description, params.Visibility,
	)
	created, err := scanVideo(row)
	if err != nil {
		return Video{}, fmt.Errorf("create video: %w", err)
	}
	return created, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, id uuid.UUID) (Video, error) {
	found, err := scanVideo(r.database.QueryRow(ctx, `
		SELECT `+videoColumns+`
		FROM videos
		WHERE id = $1 AND deleted_at IS NULL`, id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, fmt.Errorf("find video: %w", err)
	}
	return found, nil
}

func (r *PostgresRepository) ListPublic(ctx context.Context, limit int, after *Cursor) ([]Video, error) {
	query := `
		SELECT ` + videoColumns + `
		FROM videos
		WHERE visibility = 'PUBLIC' AND deleted_at IS NULL`
	arguments := []any{}
	if after != nil {
		query += ` AND (created_at, id) < ($1, $2)`
		arguments = append(arguments, after.CreatedAt, after.ID)
	}
	arguments = append(arguments, limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(arguments))
	return r.queryVideos(ctx, query, arguments...)
}

func (r *PostgresRepository) ListByOwner(ctx context.Context, ownerID uuid.UUID, limit int, after *Cursor) ([]Video, error) {
	query := `
		SELECT ` + videoColumns + `
		FROM videos
		WHERE owner_id = $1 AND deleted_at IS NULL`
	arguments := []any{ownerID}
	if after != nil {
		query += ` AND (created_at, id) < ($2, $3)`
		arguments = append(arguments, after.CreatedAt, after.ID)
	}
	arguments = append(arguments, limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(arguments))
	return r.queryVideos(ctx, query, arguments...)
}

func (r *PostgresRepository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (Video, error) {
	updated, err := scanVideo(r.database.QueryRow(ctx, `
		UPDATE videos
		SET title = COALESCE($2, title),
			description = COALESCE($3, description),
			visibility = COALESCE($4, visibility),
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+videoColumns,
		id, params.Title, params.Description, params.Visibility,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, fmt.Errorf("update video: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	result, err := r.database.Exec(ctx, `
		UPDATE videos
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, id,
	)
	if err != nil {
		return fmt.Errorf("delete video: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) queryVideos(ctx context.Context, query string, arguments ...any) ([]Video, error) {
	rows, err := r.database.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query videos: %w", err)
	}
	defer rows.Close()

	videos := make([]Video, 0)
	for rows.Next() {
		found, err := scanVideo(rows)
		if err != nil {
			return nil, fmt.Errorf("scan video: %w", err)
		}
		videos = append(videos, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate videos: %w", err)
	}
	return videos, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanVideo(row rowScanner) (Video, error) {
	var found Video
	err := row.Scan(
		&found.ID,
		&found.OwnerID,
		&found.Title,
		&found.Description,
		&found.Visibility,
		&found.ProcessingStatus,
		&found.ViewCount,
		&found.PublishedAt,
		&found.CreatedAt,
		&found.UpdatedAt,
	)
	return found, err
}
