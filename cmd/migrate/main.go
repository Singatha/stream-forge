package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("path", "migrations", "directory containing SQL migrations")
	direction := flag.String("direction", "up", "migration direction: up or down")
	flag.Parse()

	databaseURL := os.Getenv("STREAMFORGE_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("STREAMFORGE_DATABASE_URL is required")
	}

	migrator, err := migrate.New("file://"+*path, databaseURL)
	if err != nil {
		return fmt.Errorf("initialize migrator: %w", err)
	}
	defer func() {
		_, _ = migrator.Close()
	}()

	switch *direction {
	case "up":
		err = migrator.Up()
	case "down":
		err = migrator.Steps(-1)
	default:
		return fmt.Errorf("unsupported migration direction %q", *direction)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	slog.Info("migration complete", "direction", *direction)
	return nil
}
