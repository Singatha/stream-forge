package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidRefreshToken = errors.New("refresh token is invalid or expired")
	ErrRefreshTokenReuse   = errors.New("refresh token reuse detected")
)

type RefreshSession struct {
	ID        uuid.UUID
	FamilyID  uuid.UUID
	UserID    uuid.UUID
	TokenHash []byte
	ExpiresAt time.Time
	CreatedAt time.Time
}

type SessionRepository interface {
	Create(context.Context, RefreshSession) error
	Rotate(context.Context, []byte, RefreshSession, time.Time) (uuid.UUID, error)
	RevokeFamilyByTokenHash(context.Context, []byte, time.Time) error
}

type PostgresSessionRepository struct {
	database *pgxpool.Pool
}

func NewPostgresSessionRepository(database *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{database: database}
}

func (r *PostgresSessionRepository) Create(ctx context.Context, session RefreshSession) error {
	_, err := r.database.Exec(ctx, `
		INSERT INTO refresh_tokens (id, family_id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		session.ID, session.FamilyID, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create refresh session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) Rotate(ctx context.Context, oldHash []byte, replacement RefreshSession, now time.Time) (uuid.UUID, error) {
	transaction, err := r.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var current RefreshSession
	var usedAt, revokedAt *time.Time
	err = transaction.QueryRow(ctx, `
		SELECT id, family_id, user_id, token_hash, expires_at, created_at, used_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`, oldHash,
	).Scan(
		&current.ID, &current.FamilyID, &current.UserID, &current.TokenHash,
		&current.ExpiresAt, &current.CreatedAt, &usedAt, &revokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalidRefreshToken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock refresh session: %w", err)
	}

	if usedAt != nil || revokedAt != nil {
		if _, err := transaction.Exec(ctx, `
			UPDATE refresh_tokens
			SET revoked_at = COALESCE(revoked_at, $2)
			WHERE family_id = $1`, current.FamilyID, now); err != nil {
			return uuid.Nil, fmt.Errorf("revoke reused refresh family: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("commit reused refresh revocation: %w", err)
		}
		return uuid.Nil, ErrRefreshTokenReuse
	}

	if !now.Before(current.ExpiresAt) {
		if _, err := transaction.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE id = $1`, current.ID, now); err != nil {
			return uuid.Nil, fmt.Errorf("expire refresh session: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("commit expired refresh session: %w", err)
		}
		return uuid.Nil, ErrInvalidRefreshToken
	}

	replacement.FamilyID = current.FamilyID
	replacement.UserID = current.UserID
	if _, err := transaction.Exec(ctx, `
		INSERT INTO refresh_tokens (id, family_id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		replacement.ID, replacement.FamilyID, replacement.UserID, replacement.TokenHash, replacement.ExpiresAt, replacement.CreatedAt,
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert rotated refresh session: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE refresh_tokens
		SET used_at = $2, replaced_by_id = $3
		WHERE id = $1`, current.ID, now, replacement.ID,
	); err != nil {
		return uuid.Nil, fmt.Errorf("consume refresh session: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit refresh rotation: %w", err)
	}
	return current.UserID, nil
}

func (r *PostgresSessionRepository) RevokeFamilyByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) error {
	_, err := r.database.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = COALESCE(revoked_at, $2)
		WHERE family_id = (
			SELECT family_id FROM refresh_tokens WHERE token_hash = $1
		)`, tokenHash, now,
	)
	if err != nil {
		return fmt.Errorf("revoke refresh family: %w", err)
	}
	return nil
}
