package repository

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"config-service/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

//go:embed schema.sql
var schemaSQL string

// Postgres is a database/sql-backed Repository implementation for
// PostgreSQL. It is safe for concurrent use (database/sql pools connections
// internally).
type Postgres struct {
	db *sql.DB
}

// NewPostgres opens a connection pool to the given DSN and verifies
// connectivity with a bounded-time ping. It does not run migrations; call
// Migrate separately so callers can control startup ordering and logging.
func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Postgres{db: db}, nil
}

// Migrate applies the embedded schema. It is idempotent (uses CREATE TABLE
// IF NOT EXISTS / CREATE INDEX IF NOT EXISTS) and safe to run on every
// startup, which is how it is invoked from cmd/main.go.
func (p *Postgres) Migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Close releases the underlying connection pool.
func (p *Postgres) Close() error {
	return p.db.Close()
}

// Ping verifies the database connection is alive, used by the /readyz
// endpoint to signal readiness independently from process liveness.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// Get retrieves a Config by its ID. Returns ErrNotFound when absent.
func (p *Postgres) Get(ctx context.Context, id string) (*domain.Config, error) {
	const q = `
		SELECT id, host, port, app_name, log_level, created_at, updated_at
		FROM configs
		WHERE id = $1`

	var cfg domain.Config
	var createdAt, updatedAt time.Time

	err := p.db.QueryRowContext(ctx, q, id).Scan(
		&cfg.ID, &cfg.Host, &cfg.Port, &cfg.AppName, &cfg.LogLevel,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query config %q: %w", id, err)
	}

	cfg.CreatedAt = &createdAt
	cfg.UpdatedAt = &updatedAt

	return &cfg, nil
}

// Upsert creates or updates a Config record, refreshing updated_at on every
// write and preserving the original created_at on conflict.
func (p *Postgres) Upsert(ctx context.Context, cfg *domain.Config) error {
	const q = `
		INSERT INTO configs (id, host, port, app_name, log_level)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			host       = EXCLUDED.host,
			port       = EXCLUDED.port,
			app_name   = EXCLUDED.app_name,
			log_level  = EXCLUDED.log_level,
			updated_at = now()`

	if _, err := p.db.ExecContext(ctx, q,
		cfg.ID, cfg.Host, cfg.Port, cfg.AppName, cfg.LogLevel,
	); err != nil {
		return fmt.Errorf("upsert config %q: %w", cfg.ID, err)
	}

	return nil
}
