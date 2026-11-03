package video

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

type Service struct {
	repository Repository
}

type CreateInput struct {
	Title       string
	Description string
	Visibility  string
}

type UpdateInput struct {
	Title       *string
	Description *string
	Visibility  *string
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, actor Actor, input CreateInput) (Video, error) {
	if actor.UserID == uuid.Nil {
		return Video{}, ErrForbidden
	}
	params, err := validateCreate(input)
	if err != nil {
		return Video{}, err
	}
	params.OwnerID = actor.UserID
	return s.repository.Create(ctx, params)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID, actor *Actor) (Video, error) {
	found, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Video{}, err
	}
	if found.Visibility == VisibilityPrivate && !canManage(found, actor) {
		return Video{}, ErrNotFound
	}
	return found, nil
}

func (s *Service) ListPublic(ctx context.Context, limit int, cursor *Cursor) (Page, error) {
	return s.list(ctx, limit, func(fetchLimit int) ([]Video, error) {
		return s.repository.ListPublic(ctx, fetchLimit, cursor)
	})
}

func (s *Service) ListOwned(ctx context.Context, actor Actor, limit int, cursor *Cursor) (Page, error) {
	if actor.UserID == uuid.Nil {
		return Page{}, ErrForbidden
	}
	return s.list(ctx, limit, func(fetchLimit int) ([]Video, error) {
		return s.repository.ListByOwner(ctx, actor.UserID, fetchLimit, cursor)
	})
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, actor Actor, input UpdateInput) (Video, error) {
	params, err := validateUpdate(input)
	if err != nil {
		return Video{}, err
	}
	found, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Video{}, err
	}
	if !canManage(found, &actor) {
		return Video{}, ErrForbidden
	}
	return s.repository.Update(ctx, id, params)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID, actor Actor) error {
	found, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !canManage(found, &actor) {
		return ErrForbidden
	}
	return s.repository.SoftDelete(ctx, id)
}

func (s *Service) list(ctx context.Context, limit int, fetch func(int) ([]Video, error)) (Page, error) {
	if limit < 1 || limit > MaxPageSize {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, MaxPageSize)
	}
	items, err := fetch(limit + 1)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
	}
	return page, nil
}

func validateCreate(input CreateInput) (CreateParams, error) {
	title, err := normalizeTitle(input.Title)
	if err != nil {
		return CreateParams{}, err
	}
	description, err := normalizeDescription(input.Description)
	if err != nil {
		return CreateParams{}, err
	}
	visibility := strings.ToUpper(strings.TrimSpace(input.Visibility))
	if visibility == "" {
		visibility = VisibilityPrivate
	}
	if !validVisibility(visibility) {
		return CreateParams{}, fmt.Errorf("%w: visibility must be PUBLIC, PRIVATE, or UNLISTED", ErrInvalidInput)
	}
	return CreateParams{Title: title, Description: description, Visibility: visibility}, nil
}

func validateUpdate(input UpdateInput) (UpdateParams, error) {
	if input.Title == nil && input.Description == nil && input.Visibility == nil {
		return UpdateParams{}, fmt.Errorf("%w: at least one editable field is required", ErrInvalidInput)
	}
	params := UpdateParams{}
	if input.Title != nil {
		title, err := normalizeTitle(*input.Title)
		if err != nil {
			return UpdateParams{}, err
		}
		params.Title = &title
	}
	if input.Description != nil {
		description, err := normalizeDescription(*input.Description)
		if err != nil {
			return UpdateParams{}, err
		}
		params.Description = &description
	}
	if input.Visibility != nil {
		visibility := strings.ToUpper(strings.TrimSpace(*input.Visibility))
		if !validVisibility(visibility) {
			return UpdateParams{}, fmt.Errorf("%w: visibility must be PUBLIC, PRIVATE, or UNLISTED", ErrInvalidInput)
		}
		params.Visibility = &visibility
	}
	return params, nil
}

func normalizeTitle(value string) (string, error) {
	value = strings.TrimSpace(value)
	if length := utf8.RuneCountInString(value); length < 1 || length > 200 {
		return "", fmt.Errorf("%w: title must contain between 1 and 200 characters", ErrInvalidInput)
	}
	return value, nil
}

func normalizeDescription(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > 5000 {
		return "", fmt.Errorf("%w: description must not exceed 5000 characters", ErrInvalidInput)
	}
	return value, nil
}

func validVisibility(value string) bool {
	return value == VisibilityPublic || value == VisibilityPrivate || value == VisibilityUnlisted
}

func canManage(found Video, actor *Actor) bool {
	return actor != nil && (actor.UserID == found.OwnerID || actor.Role == user.RoleAdmin)
}

func IsValidationError(err error) bool {
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrInvalidCursor)
}
