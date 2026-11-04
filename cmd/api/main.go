package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/streamforge/streamforge/internal/auth"
	"github.com/streamforge/streamforge/internal/config"
	"github.com/streamforge/streamforge/internal/health"
	"github.com/streamforge/streamforge/internal/platform/database"
	"github.com/streamforge/streamforge/internal/platform/httpserver"
	"github.com/streamforge/streamforge/internal/platform/observability"
	"github.com/streamforge/streamforge/internal/storage"
	"github.com/streamforge/streamforge/internal/upload"
	"github.com/streamforge/streamforge/internal/user"
	"github.com/streamforge/streamforge/internal/video"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	userRepository := user.NewPostgresRepository(pool)
	userService := user.NewService(userRepository)
	passwordHasher := auth.NewArgon2idHasher()
	tokenManager := auth.NewJWTTokenManager(cfg.Auth.JWTIssuer, cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL)
	sessionRepository := auth.NewPostgresSessionRepository(pool)
	authService, err := auth.NewService(userRepository, sessionRepository, passwordHasher, tokenManager, cfg.Auth.RefreshTokenTTL)
	if err != nil {
		return err
	}
	authMiddleware := auth.NewMiddleware(tokenManager)
	loginLimiter := httpserver.NewIPRateLimiter(cfg.Auth.LoginRateLimit, cfg.Auth.LoginRateWindow)
	registerLimiter := httpserver.NewIPRateLimiter(cfg.Auth.RegisterRateLimit, cfg.Auth.RegisterRateWindow)
	authHandler := auth.NewHandler(authService, loginLimiter.Middleware, registerLimiter.Middleware)
	userHandler := user.NewHandler(userService, authMiddleware.Authenticate, auth.UserIDFromContext)
	videoRepository := video.NewPostgresRepository(pool)
	videoService := video.NewService(videoRepository)
	videoHandler := video.NewHandler(videoService, authMiddleware.Authenticate, authMiddleware.OptionalAuthenticate)
	objectStore, err := storage.NewMinIOStore(storage.MinIOConfig{
		Endpoint: cfg.Storage.Endpoint, PublicEndpoint: cfg.Storage.PublicEndpoint,
		AccessKey: cfg.Storage.AccessKey, SecretKey: cfg.Storage.SecretKey,
		Bucket: cfg.Storage.Bucket, Region: cfg.Storage.Region,
		UseSSL: cfg.Storage.UseSSL, AutoCreate: cfg.Storage.AutoCreateBucket,
	})
	if err != nil {
		return err
	}
	storageContext, cancelStorage := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStorage()
	if err := objectStore.EnsureBucket(storageContext); err != nil {
		return err
	}
	uploadRepository := upload.NewPostgresRepository(pool)
	uploadService := upload.NewService(
		uploadRepository, videoRepository, objectStore,
		cfg.Storage.PresignTTL, cfg.Storage.MaxUploadBytes,
	)
	uploadLimiter := httpserver.NewIPRateLimiter(cfg.Storage.UploadURLRateLimit, cfg.Storage.UploadURLRateWindow)
	uploadHandler := upload.NewHandler(
		uploadService, authMiddleware.Authenticate, uploadLimiter.Middleware, upload.NewMetrics(registry),
	)

	router := httpserver.NewRouter(
		logger,
		health.NewService(pool, cfg.Database.ConnectTimeout),
		registry,
		authHandler,
		userHandler,
		videoHandler,
		uploadHandler,
	)
	server := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API listening", "address", cfg.HTTP.Address, "environment", cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down API")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, server.Close())
		}
		return nil
	}
}
