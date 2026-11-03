package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type serviceRepository struct {
	updated UpdateProfileParams
	user    User
}

func (r *serviceRepository) Create(context.Context, CreateParams) (User, error) { return User{}, nil }
func (r *serviceRepository) FindByID(context.Context, uuid.UUID) (User, error)  { return r.user, nil }
func (r *serviceRepository) FindByEmail(context.Context, string) (User, error)  { return User{}, nil }
func (r *serviceRepository) FindByUsername(context.Context, string) (User, error) {
	return r.user, nil
}
func (r *serviceRepository) UpdateProfile(_ context.Context, _ uuid.UUID, params UpdateProfileParams) (User, error) {
	r.updated = params
	return r.user, nil
}

func TestUpdateProfileValidationAndNormalization(t *testing.T) {
	repository := &serviceRepository{user: User{ID: uuid.New()}}
	service := NewService(repository)
	if _, err := service.UpdateProfile(context.Background(), repository.user.ID, UpdateProfileParams{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateProfile(empty) error = %v", err)
	}
	displayName := "  Forge User  "
	bio := "  Building videos  "
	if _, err := service.UpdateProfile(context.Background(), repository.user.ID, UpdateProfileParams{DisplayName: &displayName, Bio: &bio}); err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if *repository.updated.DisplayName != "Forge User" || *repository.updated.Bio != "Building videos" {
		t.Fatalf("updated = %#v", repository.updated)
	}
}
