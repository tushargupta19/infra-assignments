package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"config-service/internal/handler"
	"config-service/internal/repository"
	"config-service/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	portStr := os.Getenv("APP_PORT")
	if portStr == "" {
		portStr = "8080"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		slog.Error("invalid APP_PORT, must be an integer between 1 and 65535", "value", portStr)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	repo, closeRepo, err := newRepository(ctx)
	if err != nil {
		slog.Error("failed to initialise storage", "error", err)
		os.Exit(1)
	}
	defer closeRepo()

	svc := service.New(repo)
	h := handler.New(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting config-service", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("server failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
			os.Exit(1)
		}
		slog.Info("shutdown complete")
	}
}

// newRepository chooses the storage backend based on the DATABASE_URL
// environment variable (populated from a Kubernetes Secret in-cluster).
//
//   - DATABASE_URL set:   connect to PostgreSQL, retrying with backoff so the
//     app tolerates the database becoming ready after the app pod starts,
//     then apply the (idempotent) schema.
//   - DATABASE_URL unset: fall back to an in-memory repository. This keeps
//     `go run ./cmd` usable for local development without a database, but
//     means data does not survive a restart — see README "Known limitations".
func newRepository(ctx context.Context) (repository.Repository, func(), error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Warn("DATABASE_URL not set, using in-memory repository (data will not persist)")
		repo := repository.NewInMemory()
		return repo, func() {}, nil
	}

	const (
		maxAttempts = 10
		baseDelay   = 500 * time.Millisecond
		maxDelay    = 10 * time.Second
	)

	var (
		pg  *repository.Postgres
		err error
	)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pg, err = repository.NewPostgres(ctx, dsn)
		if err == nil {
			break
		}

		slog.Warn("database not reachable yet, retrying",
			"attempt", attempt, "max_attempts", maxAttempts, "error", err)

		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		if delay > maxDelay {
			delay = maxDelay
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("interrupted while waiting for database: %w", ctx.Err())
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connect to postgres after %d attempts: %w", maxAttempts, err)
	}

	if err := pg.Migrate(ctx); err != nil {
		_ = pg.Close()
		return nil, nil, fmt.Errorf("migrate schema: %w", err)
	}

	slog.Info("connected to postgres and applied schema")

	return pg, func() { _ = pg.Close() }, nil
}
