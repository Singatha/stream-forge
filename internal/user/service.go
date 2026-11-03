package user

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) PublicProfile(ctx context.Context, username string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return User{}, fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	return s.repository.FindByUsername(ctx, username)
}

func (s *Service) Me(ctx context.Context, id uuid.UUID) (User, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, params UpdateProfileParams) (User, error) {
	if params.DisplayName == nil && params.Bio == nil {
		return User{}, fmt.Errorf("%w: at least one profile field is required", ErrInvalidInput)
	}
	if params.DisplayName != nil {
		value := strings.TrimSpace(*params.DisplayName)
		if length := utf8.RuneCountInString(value); length < 1 || length > 80 {
			return User{}, fmt.Errorf("%w: display_name must contain between 1 and 80 characters", ErrInvalidInput)
		}
		params.DisplayName = &value
	}
	if params.Bio != nil {
		value := strings.TrimSpace(*params.Bio)
		if utf8.RuneCountInString(value) > 500 {
			return User{}, fmt.Errorf("%w: bio must not exceed 500 characters", ErrInvalidInput)
		}
		params.Bio = &value
	}
	return s.repository.UpdateProfile(ctx, id, params)
}
