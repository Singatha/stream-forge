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
	Auth            Auth
	Storage         Storage
	ShutdownTimeout time.Duration
}

type Storage struct {
	Endpoint            string
	PublicEndpoint      string
	AccessKey           string
	SecretKey           string
	Bucket              string
	Region              string
	UseSSL              bool
	AutoCreateBucket    bool
	PresignTTL          time.Duration
	MaxUploadBytes      int64
	UploadURLRateLimit  int
	UploadURLRateWindow time.Duration
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

type Auth struct {
	JWTIssuer          string
	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	LoginRateLimit     int
	LoginRateWindow    time.Duration
	RegisterRateLimit  int
	RegisterRateWindow time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var errs []error

	cfg.Environment = env("STREAMFORGE_ENV", "development")
	cfg.LogLevel = env("STREAMFORGE_LOG_LEVEL", "info")
	cfg.HTTP.Address = env("STREAMFORGE_HTTP_ADDR", ":8080")
	cfg.Auth.JWTIssuer = strings.TrimSpace(env("STREAMFORGE_AUTH_JWT_ISSUER", "streamforge"))
	cfg.Auth.JWTSecret = os.Getenv("STREAMFORGE_AUTH_JWT_SECRET")
	cfg.Storage.Endpoint = strings.TrimSpace(env("STREAMFORGE_STORAGE_ENDPOINT", "minio:9000"))
	cfg.Storage.PublicEndpoint = strings.TrimSpace(env("STREAMFORGE_STORAGE_PUBLIC_ENDPOINT", "localhost:9000"))
	cfg.Storage.AccessKey = strings.TrimSpace(env("STREAMFORGE_STORAGE_ACCESS_KEY", "streamforge"))
	cfg.Storage.SecretKey = env("STREAMFORGE_STORAGE_SECRET_KEY", "streamforge-local-secret")
	cfg.Storage.Bucket = strings.TrimSpace(env("STREAMFORGE_STORAGE_BUCKET", "streamforge-videos"))
	cfg.Storage.Region = strings.TrimSpace(env("STREAMFORGE_STORAGE_REGION", "us-east-1"))
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
	parseInt := func(name, fallback string, target *int) {
		value, err := strconv.Atoi(env(name, fallback))
		if err != nil || value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer", name))
			return
		}
		*target = value
	}
	parseInt64 := func(name, fallback string, target *int64) {
		value, err := strconv.ParseInt(env(name, fallback), 10, 64)
		if err != nil || value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer", name))
			return
		}
		*target = value
	}
	parseBool := func(name, fallback string, target *bool) {
		value, err := strconv.ParseBool(env(name, fallback))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be true or false", name))
			return
		}
		*target = value
	}

	parseDuration("STREAMFORGE_HTTP_READ_HEADER_TIMEOUT", "5s", &cfg.HTTP.ReadHeaderTimeout)
	parseDuration("STREAMFORGE_HTTP_READ_TIMEOUT", "10s", &cfg.HTTP.ReadTimeout)
	parseDuration("STREAMFORGE_HTTP_WRITE_TIMEOUT", "15s", &cfg.HTTP.WriteTimeout)
	parseDuration("STREAMFORGE_HTTP_IDLE_TIMEOUT", "60s", &cfg.HTTP.IdleTimeout)
	parseDuration("STREAMFORGE_SHUTDOWN_TIMEOUT", "15s", &cfg.ShutdownTimeout)
	parseDuration("STREAMFORGE_DATABASE_CONNECT_TIMEOUT", "5s", &cfg.Database.ConnectTimeout)
	parseDuration("STREAMFORGE_AUTH_ACCESS_TOKEN_TTL", "15m", &cfg.Auth.AccessTokenTTL)
	parseDuration("STREAMFORGE_AUTH_REFRESH_TOKEN_TTL", "720h", &cfg.Auth.RefreshTokenTTL)
	parseDuration("STREAMFORGE_AUTH_LOGIN_RATE_WINDOW", "1m", &cfg.Auth.LoginRateWindow)
	parseDuration("STREAMFORGE_AUTH_REGISTER_RATE_WINDOW", "1h", &cfg.Auth.RegisterRateWindow)
	parseDuration("STREAMFORGE_STORAGE_PRESIGN_TTL", "15m", &cfg.Storage.PresignTTL)
	parseDuration("STREAMFORGE_STORAGE_UPLOAD_URL_RATE_WINDOW", "1m", &cfg.Storage.UploadURLRateWindow)
	parseInt32("STREAMFORGE_DATABASE_MAX_CONNS", "20", &cfg.Database.MaxConnections)
	parseInt32("STREAMFORGE_DATABASE_MIN_CONNS", "2", &cfg.Database.MinConnections)
	parseInt("STREAMFORGE_AUTH_LOGIN_RATE_LIMIT", "5", &cfg.Auth.LoginRateLimit)
	parseInt("STREAMFORGE_AUTH_REGISTER_RATE_LIMIT", "3", &cfg.Auth.RegisterRateLimit)
	parseInt("STREAMFORGE_STORAGE_UPLOAD_URL_RATE_LIMIT", "10", &cfg.Storage.UploadURLRateLimit)
	parseInt64("STREAMFORGE_STORAGE_MAX_UPLOAD_BYTES", "5368709120", &cfg.Storage.MaxUploadBytes)
	parseBool("STREAMFORGE_STORAGE_USE_SSL", "false", &cfg.Storage.UseSSL)
	parseBool("STREAMFORGE_STORAGE_AUTO_CREATE_BUCKET", "true", &cfg.Storage.AutoCreateBucket)

	if cfg.Database.MaxConnections == 0 {
		errs = append(errs, errors.New("STREAMFORGE_DATABASE_MAX_CONNS must be greater than zero"))
	}
	if cfg.Database.MinConnections > cfg.Database.MaxConnections {
		errs = append(errs, errors.New("STREAMFORGE_DATABASE_MIN_CONNS cannot exceed STREAMFORGE_DATABASE_MAX_CONNS"))
	}
	if cfg.Auth.JWTIssuer == "" {
		errs = append(errs, errors.New("STREAMFORGE_AUTH_JWT_ISSUER cannot be empty"))
	}
	if len(cfg.Auth.JWTSecret) < 32 {
		errs = append(errs, errors.New("STREAMFORGE_AUTH_JWT_SECRET must contain at least 32 bytes"))
	}
	if cfg.Storage.Endpoint == "" || cfg.Storage.PublicEndpoint == "" {
		errs = append(errs, errors.New("storage endpoints cannot be empty"))
	}
	if cfg.Storage.AccessKey == "" || cfg.Storage.SecretKey == "" {
		errs = append(errs, errors.New("storage credentials cannot be empty"))
	}
	if cfg.Storage.Bucket == "" {
		errs = append(errs, errors.New("STREAMFORGE_STORAGE_BUCKET cannot be empty"))
	}
	if cfg.Storage.Region == "" {
		errs = append(errs, errors.New("STREAMFORGE_STORAGE_REGION cannot be empty"))
	}
	if cfg.Storage.PresignTTL > time.Hour {
		errs = append(errs, errors.New("STREAMFORGE_STORAGE_PRESIGN_TTL cannot exceed 1h"))
	}

	return cfg, errors.Join(errs...)
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}
