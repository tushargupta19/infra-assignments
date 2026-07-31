package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"config-service/internal/domain"
	"config-service/internal/repository"
	"config-service/internal/service"
)

// Handler wires HTTP routes to service calls.
type Handler struct {
	svc *service.Service
}

// New creates a Handler backed by the given service.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes attaches all routes to mux.
//
// /ping is a pure liveness check (process is up and responding) and never
// touches the database. /readyz additionally verifies the storage backend
// is reachable, so it can be used as a Kubernetes readiness probe to keep a
// pod out of the Service endpoints while the database is unavailable.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ping", h.ping)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /configs/{id}", h.getConfig)
	mux.HandleFunc("POST /configs", h.upsertConfig)
}

func (h *Handler) ping(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("pong"))
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Ready(r.Context()); err != nil {
		slog.Warn("readiness check failed", "error", err)
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	cfg, err := h.svc.GetConfig(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "config not found", http.StatusNotFound)
			return
		}
		slog.Error("get config failed", "id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (h *Handler) upsertConfig(w http.ResponseWriter, r *http.Request) {
	var cfg domain.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.svc.UpsertConfig(r.Context(), &cfg); err != nil {
		var verr *domain.ValidationError
		if errors.As(err, &verr) {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("upsert config failed", "id", cfg.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(cfg)
}
