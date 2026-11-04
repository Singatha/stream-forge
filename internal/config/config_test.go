package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("STREAMFORGE_DATABASE_URL", "postgres://example")
	t.Setenv("STREAMFORGE_AUTH_JWT_SECRET", "test-only-secret-with-at-least-32-bytes")
	t.Setenv("STREAMFORGE_HTTP_READ_TIMEOUT", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.ReadTimeout != 3*time.Second {
		t.Fatalf("ReadTimeout = %v, want 3s", cfg.HTTP.ReadTimeout)
	}
	if cfg.Database.MaxConnections != 20 {
		t.Fatalf("MaxConnections = %d, want 20", cfg.Database.MaxConnections)
	}
	if cfg.Storage.PresignTTL != 15*time.Minute || cfg.Storage.MaxUploadBytes != 5*1024*1024*1024 {
		t.Fatalf("Storage defaults = %#v", cfg.Storage)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("STREAMFORGE_DATABASE_URL", "")
	t.Setenv("STREAMFORGE_DATABASE_MAX_CONNS", "1")
	t.Setenv("STREAMFORGE_DATABASE_MIN_CONNS", "2")
	t.Setenv("STREAMFORGE_AUTH_JWT_SECRET", "short")
	t.Setenv("STREAMFORGE_STORAGE_PRESIGN_TTL", "2h")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "MIN_CONNS") || !strings.Contains(err.Error(), "JWT_SECRET") || !strings.Contains(err.Error(), "PRESIGN_TTL") {
		t.Fatalf("Load() error = %q, want all validation failures", err)
	}
}
