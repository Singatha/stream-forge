package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(context.Context, CreateParams) (User, error)
	FindByID(context.Context, uuid.UUID) (User, error)
	FindByEmail(context.Context, string) (User, error)
	FindByUsername(context.Context, string) (User, error)
	UpdateProfile(context.Context, uuid.UUID, UpdateProfileParams) (User, error)
}

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

const userColumns = `id, username::TEXT, email::TEXT, password_hash, role, display_name, bio, created_at, updated_at`

func (r *PostgresRepository) Create(ctx context.Context, params CreateParams) (User, error) {
	row := r.database.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, role, display_name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+userColumns,
		params.Username, params.Email, params.PasswordHash, params.Role, params.DisplayName,
	)
	created, err := scanUser(row)
	if err != nil {
		return User{}, mapWriteError(err)
	}
	return created, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, id uuid.UUID) (User, error) {
	return r.findOne(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
}

func (r *PostgresRepository) FindByEmail(ctx context.Context, email string) (User, error) {
	return r.findOne(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email)
}

func (r *PostgresRepository) FindByUsername(ctx context.Context, username string) (User, error) {
	return r.findOne(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username)
}

func (r *PostgresRepository) UpdateProfile(ctx context.Context, id uuid.UUID, params UpdateProfileParams) (User, error) {
	row := r.database.QueryRow(ctx, `
		UPDATE users
		SET display_name = COALESCE($2, display_name),
			bio = COALESCE($3, bio),
			updated_at = NOW()
		WHERE id = $1
		RETURNING `+userColumns,
		id, params.DisplayName, params.Bio,
	)
	updated, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("update user profile: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) findOne(ctx context.Context, query string, argument any) (User, error) {
	found, err := scanUser(r.database.QueryRow(ctx, query, argument))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("query user: %w", err)
	}
	return found, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanUser(row rowScanner) (User, error) {
	var found User
	err := row.Scan(
		&found.ID,
		&found.Username,
		&found.Email,
		&found.PasswordHash,
		&found.Role,
		&found.DisplayName,
		&found.Bio,
		&found.CreatedAt,
		&found.UpdatedAt,
	)
	return found, err
}

func mapWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "users_email_key":
			return ErrEmailTaken
		case "users_username_key":
			return ErrUsernameTaken
		}
	}
	return fmt.Errorf("write user: %w", err)
}
