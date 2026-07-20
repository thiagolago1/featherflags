// Package server wires the HTTP API: admin routes (Bearer token) and the
// public evaluate endpoint (per-environment API key).
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/thiagolago1/featherflags/api/internal/evaluate"
	"github.com/thiagolago1/featherflags/api/internal/store"
)

// maxFieldLen bounds free-text admin input (project/flag names, descriptions)
// to keep payloads sane; it's not a business rule, just abuse prevention.
const maxFieldLen = 256

// Rate limits are intentionally generous: they exist to blunt abuse/DoS, not
// to constrain legitimate traffic. Tuned per route class.
const (
	evaluateRateLimit  = 120 // requests per IP per window
	evaluateRateWindow = time.Minute
	adminRateLimit     = 60
	adminRateWindow    = time.Minute
)

type Server struct {
	store         *store.Store
	adminToken    string
	allowedOrigin string
	hub           *hub
	metrics       *metrics
	log           *slog.Logger
}

// New wires the HTTP API. allowedOrigin is the single origin permitted to
// call this API cross-origin (e.g. the dashboard BFF) — in production this
// service should also sit behind a network policy that blocks everything but
// that caller, so CORS here is defense in depth, not the primary boundary.
func New(st *store.Store, adminToken, allowedOrigin string) http.Handler {
	s := &Server{
		store:         st,
		adminToken:    adminToken,
		allowedOrigin: allowedOrigin,
		hub:           newHub(),
		metrics:       &metrics{},
		log:           slog.Default(),
	}
	st.SetOnChange(s.hub.broadcast)

	r := chi.NewRouter()
	r.Use(s.cors)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(s.metrics.middleware)

	r.Get("/metrics", s.metrics.handler())

	// Long-lived SSE connection — must stay outside the request timeout.
	r.With(s.rateLimit("stream", evaluateRateLimit, evaluateRateWindow)).Get("/v1/stream", s.streamHandler)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(10 * time.Second))

		r.Get("/health", s.health)
		r.With(s.rateLimit("evaluate", evaluateRateLimit, evaluateRateWindow)).
			Post("/v1/evaluate", s.evaluateHandler)

		r.Route("/admin", func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Use(s.rateLimit("admin", adminRateLimit, adminRateWindow))
			r.Post("/projects", s.createProject)
			r.Get("/projects", s.listProjects)
			r.Post("/projects/{projectID}/flags", s.createFlag)
			r.Get("/projects/{projectID}/flags", s.listFlags)
			r.Patch("/flags/{flagID}/rules/{env}", s.updateRule)
			r.Post("/flags/{flagID}/archive", s.archiveFlag)
			r.Post("/flags/{flagID}/unarchive", s.unarchiveFlag)
		})
	})

	return r
}

// streamHandler is the SSE feed: one "change" event whenever any flag in the
// key's project is written. Clients respond by re-calling /v1/evaluate.
// The API key may come via header or query (EventSource can't set headers).
func (s *Server) streamHandler(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("apiKey")
	}
	if apiKey == "" {
		writeErr(w, http.StatusUnauthorized, "missing API key")
		return
	}
	projectID, _, err := s.store.ResolveAPIKey(r.Context(), apiKey)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusUnauthorized, "invalid API key")
		return
	}
	if err != nil {
		internalErr(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	changes, cancel := s.hub.subscribe(projectID)
	defer cancel()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			fmt.Fprint(w, "event: change\ndata: {}\n\n")
			flusher.Flush()
		case <-heartbeat.C:
			// Comment line keeps idle connections alive through proxies.
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// cors only allows the configured origin (the dashboard BFF) to read admin
// responses cross-origin. SDK traffic (evaluate/stream) is credentialed by
// API key and not cookie-based, so it's unaffected by CORS either way — this
// header only governs what a browser will expose to JS.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.allowedOrigin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger emits one structured JSON log line per request.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.log.Info("request",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// rateLimit throttles requests per client IP within the given window. name
// namespaces the counter per route class so /admin and /v1/evaluate don't
// share a budget.
func (s *Server) rateLimit(name string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !s.store.Allow(r.Context(), name+":"+ip, limit, window) {
				s.metrics.rateLimitedHits.Add(1)
				writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if i := strings.LastIndex(ip, ":"); i != -1 {
		ip = ip[:i]
	}
	return ip
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
	if len(body.Name) > maxFieldLen {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("name must be at most %d characters", maxFieldLen))
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
	if len(body.Key) > maxFieldLen || (body.Description != nil && len(*body.Description) > maxFieldLen) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("key/description must be at most %d characters", maxFieldLen))
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
	slog.Error("internal error", "error", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}
