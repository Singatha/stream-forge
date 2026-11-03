package video

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

type serviceRepository struct {
	created   CreateParams
	found     Video
	findErr   error
	updated   UpdateParams
	listed    []Video
	listLimit int
	deletedID uuid.UUID
}

func (r *serviceRepository) Create(_ context.Context, params CreateParams) (Video, error) {
	r.created = params
	created := r.found
	created.OwnerID = params.OwnerID
	created.Title = params.Title
	created.Description = params.Description
	created.Visibility = params.Visibility
	return created, nil
}
func (r *serviceRepository) FindByID(context.Context, uuid.UUID) (Video, error) {
	return r.found, r.findErr
}
func (r *serviceRepository) ListPublic(_ context.Context, limit int, _ *Cursor) ([]Video, error) {
	r.listLimit = limit
	return r.listed, nil
}
func (r *serviceRepository) ListByOwner(_ context.Context, _ uuid.UUID, limit int, _ *Cursor) ([]Video, error) {
	r.listLimit = limit
	return r.listed, nil
}
func (r *serviceRepository) Update(_ context.Context, _ uuid.UUID, params UpdateParams) (Video, error) {
	r.updated = params
	return r.found, nil
}
func (r *serviceRepository) SoftDelete(_ context.Context, id uuid.UUID) error {
	r.deletedID = id
	return nil
}

func TestCreateDefaultsToPrivateAndNormalizes(t *testing.T) {
	repository := &serviceRepository{found: Video{ID: uuid.New()}}
	service := NewService(repository)
	actor := Actor{UserID: uuid.New(), Role: user.RoleUser}
	_, err := service.Create(context.Background(), actor, CreateInput{Title: "  First video  ", Description: "  Description  "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.created.OwnerID != actor.UserID || repository.created.Title != "First video" ||
		repository.created.Description != "Description" || repository.created.Visibility != VisibilityPrivate {
		t.Fatalf("Create() params = %#v", repository.created)
	}
}

func TestPrivateVideoAuthorization(t *testing.T) {
	ownerID := uuid.New()
	repository := &serviceRepository{found: Video{ID: uuid.New(), OwnerID: ownerID, Visibility: VisibilityPrivate}}
	service := NewService(repository)

	if _, err := service.Get(context.Background(), repository.found.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(anonymous private) error = %v", err)
	}
	other := Actor{UserID: uuid.New(), Role: user.RoleUser}
	if _, err := service.Get(context.Background(), repository.found.ID, &other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(other private) error = %v", err)
	}
	owner := Actor{UserID: ownerID, Role: user.RoleUser}
	if _, err := service.Get(context.Background(), repository.found.ID, &owner); err != nil {
		t.Fatalf("Get(owner private) error = %v", err)
	}
	admin := Actor{UserID: uuid.New(), Role: user.RoleAdmin}
	if _, err := service.Get(context.Background(), repository.found.ID, &admin); err != nil {
		t.Fatalf("Get(admin private) error = %v", err)
	}
}

func TestMutationRequiresOwnerOrAdmin(t *testing.T) {
	ownerID := uuid.New()
	repository := &serviceRepository{found: Video{ID: uuid.New(), OwnerID: ownerID, Visibility: VisibilityPrivate}}
	service := NewService(repository)
	title := "Updated title"

	other := Actor{UserID: uuid.New(), Role: user.RoleUser}
	if _, err := service.Update(context.Background(), repository.found.ID, other, UpdateInput{Title: &title}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Update(other) error = %v", err)
	}
	admin := Actor{UserID: uuid.New(), Role: user.RoleAdmin}
	if _, err := service.Update(context.Background(), repository.found.ID, admin, UpdateInput{Title: &title}); err != nil {
		t.Fatalf("Update(admin) error = %v", err)
	}
	if repository.updated.Title == nil || *repository.updated.Title != title {
		t.Fatalf("Update() params = %#v", repository.updated)
	}
	if err := service.Delete(context.Background(), repository.found.ID, other); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Delete(other) error = %v", err)
	}
	if err := service.Delete(context.Background(), repository.found.ID, Actor{UserID: ownerID, Role: user.RoleUser}); err != nil {
		t.Fatalf("Delete(owner) error = %v", err)
	}
}

func TestCursorPageUsesLimitPlusOne(t *testing.T) {
	now := time.Now().UTC()
	repository := &serviceRepository{listed: []Video{
		{ID: uuid.New(), CreatedAt: now},
		{ID: uuid.New(), CreatedAt: now.Add(-time.Second)},
		{ID: uuid.New(), CreatedAt: now.Add(-2 * time.Second)},
	}}
	service := NewService(repository)
	page, err := service.ListPublic(context.Background(), 2, nil)
	if err != nil {
		t.Fatalf("ListPublic() error = %v", err)
	}
	if repository.listLimit != 3 || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("page = %#v, fetch limit = %d", page, repository.listLimit)
	}
}

func TestVideoValidation(t *testing.T) {
	repository := &serviceRepository{}
	service := NewService(repository)
	actor := Actor{UserID: uuid.New(), Role: user.RoleUser}
	if _, err := service.Create(context.Background(), actor, CreateInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create(empty) error = %v", err)
	}
	if _, err := service.Update(context.Background(), uuid.New(), actor, UpdateInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update(empty) error = %v", err)
	}
}
