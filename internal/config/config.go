package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment     string
	LogLevel        string
	HTTP            HTTP
	Database        Database
	ShutdownTimeout time.Duration
}

type HTTP struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type Database struct {
	URL            string
	MaxConnections int32
	MinConnections int32
	ConnectTimeout time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var errs []error

	cfg.Environment = env("STREAMFORGE_ENV", "development")
	cfg.LogLevel = env("STREAMFORGE_LOG_LEVEL", "info")
	cfg.HTTP.Address = env("STREAMFORGE_HTTP_ADDR", ":8080")
	cfg.Database.URL = strings.TrimSpace(os.Getenv("STREAMFORGE_DATABASE_URL"))
	if cfg.Database.URL == "" {
		errs = append(errs, errors.New("STREAMFORGE_DATABASE_URL is required"))
	}

	parseDuration := func(name, fallback string, target *time.Duration) {
		value, err := time.ParseDuration(env(name, fallback))
		if err != nil || value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive duration", name))
			return
		}
		*target = value
	}
	parseInt32 := func(name, fallback string, target *int32) {
		value, err := strconv.ParseInt(env(name, fallback), 10, 32)
		if err != nil || value < 0 {
			errs = append(errs, fmt.Errorf("%s must be a non-negative integer", name))
			return
		}
		*target = int32(value)
	}

	parseDuration("STREAMFORGE_HTTP_READ_HEADER_TIMEOUT", "5s", &cfg.HTTP.ReadHeaderTimeout)
	parseDuration("STREAMFORGE_HTTP_READ_TIMEOUT", "10s", &cfg.HTTP.ReadTimeout)
	parseDuration("STREAMFORGE_HTTP_WRITE_TIMEOUT", "15s", &cfg.HTTP.WriteTimeout)
	parseDuration("STREAMFORGE_HTTP_IDLE_TIMEOUT", "60s", &cfg.HTTP.IdleTimeout)
	parseDuration("STREAMFORGE_SHUTDOWN_TIMEOUT", "15s", &cfg.ShutdownTimeout)
	parseDuration("STREAMFORGE_DATABASE_CONNECT_TIMEOUT", "5s", &cfg.Database.ConnectTimeout)
	parseInt32("STREAMFORGE_DATABASE_MAX_CONNS", "20", &cfg.Database.MaxConnections)
	parseInt32("STREAMFORGE_DATABASE_MIN_CONNS", "2", &cfg.Database.MinConnections)

	if cfg.Database.MaxConnections == 0 {
		errs = append(errs, errors.New("STREAMFORGE_DATABASE_MAX_CONNS must be greater than zero"))
	}
	if cfg.Database.MinConnections > cfg.Database.MaxConnections {
		errs = append(errs, errors.New("STREAMFORGE_DATABASE_MIN_CONNS cannot exceed STREAMFORGE_DATABASE_MAX_CONNS"))
	}

	return cfg, errors.Join(errs...)
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}
