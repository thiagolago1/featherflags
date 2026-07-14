// Package server wires the HTTP API: admin routes (Bearer token) and the
// public evaluate endpoint (per-environment API key).
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/thiagolago1/featherflags/api/internal/evaluate"
	"github.com/thiagolago1/featherflags/api/internal/store"
)

type Server struct {
	store      *store.Store
	adminToken string
}

func New(st *store.Store, adminToken string) http.Handler {
	s := &Server{store: st, adminToken: adminToken}

	r := chi.NewRouter()
	r.Use(cors)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Second))

	r.Get("/health", s.health)
	r.Post("/v1/evaluate", s.evaluateHandler)

	r.Route("/admin", func(r chi.Router) {
		r.Use(s.requireAdmin)
		r.Post("/projects", s.createProject)
		r.Get("/projects", s.listProjects)
		r.Post("/projects/{projectID}/flags", s.createFlag)
		r.Get("/projects/{projectID}/flags", s.listFlags)
		r.Patch("/flags/{flagID}/rules/{env}", s.updateRule)
		r.Post("/flags/{flagID}/archive", s.archiveFlag)
		r.Post("/flags/{flagID}/unarchive", s.unarchiveFlag)
	})

	return r
}

// cors is permissive: every route is protected by a credential header (admin
// bearer or API key), never by cookies, so cross-origin reads are safe.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		want := "Bearer " + s.adminToken
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeErr(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- evaluate ---

type evaluateRequest struct {
	UserID     string            `json:"userId"`
	Attributes map[string]string `json:"attributes"`
}

func (s *Server) evaluateHandler(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		writeErr(w, http.StatusUnauthorized, "missing X-API-Key")
		return
	}
	var req evaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "userId is required")
		return
	}

	rules, err := s.store.RulesForAPIKey(r.Context(), apiKey)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusUnauthorized, "invalid API key")
		return
	}
	if err != nil {
		internalErr(w, err)
		return
	}

	ctx := evaluate.Context{UserID: req.UserID, Attributes: req.Attributes}
	flags := make(map[string]bool, len(rules))
	for _, r := range rules {
		var conds []evaluate.Condition
		if len(r.Conditions) > 0 {
			// Malformed stored conditions fail closed via unknown-op handling.
			_ = json.Unmarshal(r.Conditions, &conds)
		}
		flags[r.FlagKey] = evaluate.Evaluate(evaluate.Rule{
			FlagKey:        r.FlagKey,
			Enabled:        r.Enabled,
			Archived:       r.Archived,
			RolloutPercent: r.RolloutPercent,
			Conditions:     conds,
		}, ctx)
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": flags})
}

// --- admin ---

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	p, err := s.store.CreateProject(r.Context(), body.Name)
	if err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListProjects(r.Context())
	if err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createFlag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key         string  `json:"key"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Key == "" {
		writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	f, err := s.store.CreateFlag(r.Context(), chi.URLParam(r, "projectID"), body.Key, body.Description)
	if err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) listFlags(w http.ResponseWriter, r *http.Request) {
	fs, err := s.store.ListFlags(r.Context(), chi.URLParam(r, "projectID"))
	if err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	env := chi.URLParam(r, "env")
	if !slices.Contains(store.Environments, env) {
		writeErr(w, http.StatusBadRequest, "env must be one of: development, staging, production")
		return
	}
	var body struct {
		Enabled        *bool           `json:"enabled"`
		RolloutPercent *int            `json:"rolloutPercent"`
		Conditions     json.RawMessage `json:"conditions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.RolloutPercent != nil && (*body.RolloutPercent < 0 || *body.RolloutPercent > 100) {
		writeErr(w, http.StatusBadRequest, "rolloutPercent must be 0-100")
		return
	}
	err := s.store.UpdateRule(r.Context(), chi.URLParam(r, "flagID"), env,
		body.Enabled, body.RolloutPercent, body.Conditions)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "flag rule not found")
		return
	}
	if err != nil {
		internalErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) archiveFlag(w http.ResponseWriter, r *http.Request)   { s.setArchived(w, r, true) }
func (s *Server) unarchiveFlag(w http.ResponseWriter, r *http.Request) { s.setArchived(w, r, false) }

func (s *Server) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	err := s.store.ArchiveFlag(r.Context(), chi.URLParam(r, "flagID"), archived)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "flag not found")
		return
	}
	if err != nil {
		internalErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func internalErr(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}
