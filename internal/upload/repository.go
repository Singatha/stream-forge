package upload

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	PrepareSource(context.Context, PrepareParams) (Asset, error)
	FindSource(context.Context, uuid.UUID) (Asset, error)
	CompleteSource(context.Context, CompleteParams) (Asset, bool, error)
}

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

const assetColumns = `id, video_id, asset_type, object_key, expected_content_type,
	expected_size_bytes, content_type, size_bytes, etag, storage_status,
	completed_at, created_at, updated_at`

func (r *PostgresRepository) PrepareSource(ctx context.Context, params PrepareParams) (Asset, error) {
	tx, err := r.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Asset{}, fmt.Errorf("begin source upload: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
		SELECT processing_status
		FROM videos
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, params.VideoID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, fmt.Errorf("lock video for upload: %w", err)
	}
	if status != "CREATED" && status != "UPLOADING" {
		return Asset{}, ErrInvalidState
	}

	asset, err := scanAsset(tx.QueryRow(ctx, `
		SELECT `+assetColumns+`
		FROM video_assets
		WHERE video_id = $1 AND asset_type = 'SOURCE'
		FOR UPDATE`, params.VideoID))
	if errors.Is(err, pgx.ErrNoRows) {
		asset, err = scanAsset(tx.QueryRow(ctx, `
			INSERT INTO video_assets (
				video_id, asset_type, object_key, expected_content_type, expected_size_bytes
			)
			VALUES ($1, 'SOURCE', $2, $3, $4)
			RETURNING `+assetColumns,
			params.VideoID, params.ObjectKey, params.ContentType, params.SizeBytes,
		))
	} else if err == nil && (asset.ObjectKey != params.ObjectKey ||
		asset.ExpectedContentType != params.ContentType || asset.ExpectedSizeBytes != params.SizeBytes) {
		return Asset{}, ErrUploadConflict
	}
	if err != nil {
		return Asset{}, fmt.Errorf("prepare source asset: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE videos SET processing_status = 'UPLOADING', updated_at = NOW()
		WHERE id = $1`, params.VideoID); err != nil {
		return Asset{}, fmt.Errorf("mark video uploading: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Asset{}, fmt.Errorf("commit source upload: %w", err)
	}
	return asset, nil
}

func (r *PostgresRepository) FindSource(ctx context.Context, videoID uuid.UUID) (Asset, error) {
	asset, err := scanAsset(r.database.QueryRow(ctx, `
		SELECT `+assetColumns+`
		FROM video_assets
		WHERE video_id = $1 AND asset_type = 'SOURCE'`, videoID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, fmt.Errorf("find source asset: %w", err)
	}
	return asset, nil
}

func (r *PostgresRepository) CompleteSource(ctx context.Context, params CompleteParams) (Asset, bool, error) {
	tx, err := r.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Asset{}, false, fmt.Errorf("begin upload completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
		SELECT processing_status
		FROM videos
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, params.VideoID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, false, ErrNotFound
	}
	if err != nil {
		return Asset{}, false, fmt.Errorf("lock video for completion: %w", err)
	}
	asset, err := scanAsset(tx.QueryRow(ctx, `
		SELECT `+assetColumns+`
		FROM video_assets
		WHERE id = $1 AND video_id = $2 AND asset_type = 'SOURCE'
		FOR UPDATE`, params.AssetID, params.VideoID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, false, ErrNotFound
	}
	if err != nil {
		return Asset{}, false, fmt.Errorf("lock source asset: %w", err)
	}
	if status == "UPLOADED" && asset.StorageStatus == AssetReady {
		if err := tx.Commit(ctx); err != nil {
			return Asset{}, false, fmt.Errorf("commit repeated completion: %w", err)
		}
		return asset, false, nil
	}
	if status != "UPLOADING" || asset.StorageStatus != AssetPending {
		return Asset{}, false, ErrInvalidState
	}

	asset, err = scanAsset(tx.QueryRow(ctx, `
		UPDATE video_assets
		SET content_type = $3, size_bytes = $4, etag = $5,
			storage_status = 'READY', completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND video_id = $2
		RETURNING `+assetColumns,
		params.AssetID, params.VideoID, params.ContentType, params.SizeBytes, params.ETag,
	))
	if err != nil {
		return Asset{}, false, fmt.Errorf("complete source asset: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE videos SET processing_status = 'UPLOADED', updated_at = NOW()
		WHERE id = $1`, params.VideoID); err != nil {
		return Asset{}, false, fmt.Errorf("mark video uploaded: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Asset{}, false, fmt.Errorf("commit upload completion: %w", err)
	}
	return asset, true, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanAsset(row rowScanner) (Asset, error) {
	var asset Asset
	err := row.Scan(
		&asset.ID, &asset.VideoID, &asset.AssetType, &asset.ObjectKey,
		&asset.ExpectedContentType, &asset.ExpectedSizeBytes, &asset.ContentType,
		&asset.SizeBytes, &asset.ETag, &asset.StorageStatus, &asset.CompletedAt,
		&asset.CreatedAt, &asset.UpdatedAt,
	)
	return asset, err
}
