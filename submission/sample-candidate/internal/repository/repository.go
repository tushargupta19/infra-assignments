package repository

import (
	"context"
	"errors"
	"sync"

	"config-service/internal/domain"
)

// ErrNotFound is returned when a config record does not exist.
var ErrNotFound = errors.New("config not found")

// Repository defines storage operations for Config records.
//
// Ping reports whether the underlying storage is reachable and is used to
// back the /readyz endpoint. It lets the handler distinguish "the process is
// alive" (liveness, /ping) from "the service can serve real traffic"
// (readiness, /readyz) without leaking storage details into the HTTP layer.
type Repository interface {
	Get(ctx context.Context, id string) (*domain.Config, error)
	Upsert(ctx context.Context, cfg *domain.Config) error
	Ping(ctx context.Context) error
}

// InMemory is a thread-safe, in-memory Repository implementation.
// It is used for local testing and as a stand-in when no database is configured.
type InMemory struct {
	mu   sync.RWMutex
	data map[string]*domain.Config
}

// NewInMemory returns an initialised InMemory repository.
func NewInMemory() *InMemory {
	return &InMemory{data: make(map[string]*domain.Config)}
}

// Get retrieves a Config by its ID. Returns ErrNotFound when absent.
func (r *InMemory) Get(_ context.Context, id string) (*domain.Config, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, ok := r.data[id]
	if !ok {
		return nil, ErrNotFound
	}

	return cfg, nil
}

// Upsert creates or replaces a Config record.
func (r *InMemory) Upsert(_ context.Context, cfg *domain.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data[cfg.ID] = cfg

	return nil
}

// Ping always succeeds for the in-memory repository: there is no external
// dependency to check.
func (r *InMemory) Ping(_ context.Context) error {
	return nil
}
