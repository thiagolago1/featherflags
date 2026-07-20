// Package store is the Postgres data layer (pgx, no ORM).
package store

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

//go:embed migrations
var migrationsFS embed.FS

var Environments = []string{"development", "staging", "production"}

var envKeyPrefix = map[string]string{
	"development": "ff_dev_",
	"staging":     "ff_stg_",
	"production":  "ff_prod_",
}

type Store struct {
	pool     *pgxpool.Pool
	redis    *redis.Client // optional; nil means no caching
	onChange func(projectID string)

	localMu      sync.Mutex
	localWindows map[string]*localWindow // used by Allow() when redis is nil
}

// SetOnChange registers a callback fired after any admin write that can alter
// evaluation results (used by the SSE hub). Must be set before serving.
func (s *Store) SetOnChange(fn func(projectID string)) { s.onChange = fn }

func (s *Store) emitChange(projectID string) {
	if s.onChange != nil {
		s.onChange(projectID)
	}
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Migrate applies embedded SQL migrations in filename order, tracking them in
// schema_migrations so reruns are no-ops.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	if err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type Project struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	APIKeys []APIKey `json:"apiKeys,omitempty"`
}

type APIKey struct {
	Key string `json:"key"`
	Env string `json:"env"`
}

type Flag struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	Description *string    `json:"description"`
	Archived    bool       `json:"archived"`
	Rules       []FlagRule `json:"rules"`
}

type FlagRule struct {
	Env            string          `json:"env"`
	Enabled        bool            `json:"enabled"`
	RolloutPercent int             `json:"rolloutPercent"`
	Conditions     json.RawMessage `json:"conditions"`
}

// CreateProject creates the project plus one API key per environment.
func (s *Store) CreateProject(ctx context.Context, name string) (*Project, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	p := &Project{ID: newID(), Name: name}
	if _, err := tx.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1,$2)`, p.ID, p.Name); err != nil {
		return nil, err
	}
	for _, env := range Environments {
		k := APIKey{Key: envKeyPrefix[env] + newID(), Env: env}
		if _, err := tx.Exec(ctx,
			`INSERT INTO api_keys (id, key, env, project_id) VALUES ($1,$2,$3,$4)`,
			newID(), k.Key, k.Env, p.ID); err != nil {
			return nil, err
		}
		p.APIKeys = append(p.APIKeys, k)
	}
	return p, tx.Commit(ctx)
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, k.key, k.env
		FROM projects p JOIN api_keys k ON k.project_id = p.id
		ORDER BY p.created_at, k.env`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[string]*Project{}
	order := []string{}
	for rows.Next() {
		var id, name string
		var k APIKey
		if err := rows.Scan(&id, &name, &k.Key, &k.Env); err != nil {
			return nil, err
		}
		p, ok := byID[id]
		if !ok {
			p = &Project{ID: id, Name: name}
			byID[id] = p
			order = append(order, id)
		}
		p.APIKeys = append(p.APIKeys, k)
	}
	out := make([]Project, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, rows.Err()
}

// CreateFlag creates the flag plus one disabled rule per environment.
func (s *Store) CreateFlag(ctx context.Context, projectID, key string, description *string) (*Flag, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	f := &Flag{ID: newID(), Key: key, Description: description}
	if _, err := tx.Exec(ctx,
		`INSERT INTO flags (id, key, description, project_id) VALUES ($1,$2,$3,$4)`,
		f.ID, f.Key, f.Description, projectID); err != nil {
		return nil, err
	}
	for _, env := range Environments {
		if _, err := tx.Exec(ctx,
			`INSERT INTO flag_rules (id, flag_id, env) VALUES ($1,$2,$3)`,
			newID(), f.ID, env); err != nil {
			return nil, err
		}
		f.Rules = append(f.Rules, FlagRule{Env: env, Enabled: false, RolloutPercent: 100})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.invalidateProject(ctx, projectID)
	s.emitChange(projectID)
	return f, nil
}

func (s *Store) ListFlags(ctx context.Context, projectID string) ([]Flag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.key, f.description, f.archived, r.env, r.enabled, r.rollout_percent, r.conditions
		FROM flags f JOIN flag_rules r ON r.flag_id = f.id
		WHERE f.project_id = $1
		ORDER BY f.created_at, r.env`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[string]*Flag{}
	order := []string{}
	for rows.Next() {
		var id string
		var f Flag
		var r FlagRule
		if err := rows.Scan(&id, &f.Key, &f.Description, &f.Archived,
			&r.Env, &r.Enabled, &r.RolloutPercent, &r.Conditions); err != nil {
			return nil, err
		}
		fl, ok := byID[id]
		if !ok {
			f.ID = id
			byID[id] = &f
			order = append(order, id)
			fl = &f
		}
		fl.Rules = append(fl.Rules, r)
	}
	out := make([]Flag, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, rows.Err()
}

var ErrNotFound = pgx.ErrNoRows

func (s *Store) UpdateRule(ctx context.Context, flagID, env string, enabled *bool, rolloutPercent *int, conditions json.RawMessage) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE flag_rules SET
			enabled         = COALESCE($3, enabled),
			rollout_percent = COALESCE($4, rollout_percent),
			conditions      = COALESCE($5, conditions),
			updated_at      = now()
		WHERE flag_id = $1 AND env = $2`,
		flagID, env, enabled, rolloutPercent, conditions)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if projectID, err := s.projectIDForFlag(ctx, flagID); err == nil {
		s.invalidateProject(ctx, projectID)
		s.emitChange(projectID)
	}
	return nil
}

func (s *Store) ArchiveFlag(ctx context.Context, flagID string, archived bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE flags SET archived = $2 WHERE id = $1`, flagID, archived)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if projectID, err := s.projectIDForFlag(ctx, flagID); err == nil {
		s.invalidateProject(ctx, projectID)
		s.emitChange(projectID)
	}
	return nil
}

// EvalRule is everything the engine needs for one flag in one environment.
type EvalRule struct {
	FlagKey        string
	Archived       bool
	Enabled        bool
	RolloutPercent int
	Conditions     json.RawMessage
}

// ResolveAPIKey maps an API key to its project and environment, served from
// Redis when available. Returns ErrNotFound for unknown keys.
func (s *Store) ResolveAPIKey(ctx context.Context, apiKey string) (projectID, env string, err error) {
	ki, hit := s.cachedKeyInfo(ctx, apiKey)
	if !hit {
		err := s.pool.QueryRow(ctx,
			`SELECT project_id, env FROM api_keys WHERE key = $1`, apiKey).Scan(&ki.ProjectID, &ki.Env)
		if err != nil {
			return "", "", err
		}
		s.storeCache(ctx, apiKeyCacheKey(apiKey), ki, apiKeyTTL)
	}
	return ki.ProjectID, ki.Env, nil
}

// RulesForAPIKey resolves an API key to its project+environment rule set,
// served from Redis when available. Returns ErrNotFound for unknown keys.
func (s *Store) RulesForAPIKey(ctx context.Context, apiKey string) ([]EvalRule, error) {
	var ki keyInfo
	var err error
	ki.ProjectID, ki.Env, err = s.ResolveAPIKey(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	if rules, ok := s.cachedRules(ctx, ki.ProjectID, ki.Env); ok {
		return rules, nil
	}
	rules, err := s.rulesFromDB(ctx, ki.ProjectID, ki.Env)
	if err != nil {
		return nil, err
	}
	s.storeCache(ctx, rulesCacheKey(ki.ProjectID, ki.Env), rules, rulesTTL)
	return rules, nil
}

func (s *Store) rulesFromDB(ctx context.Context, projectID, env string) ([]EvalRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.key, f.archived, r.enabled, r.rollout_percent, r.conditions
		FROM flags f JOIN flag_rules r ON r.flag_id = f.id AND r.env = $2
		WHERE f.project_id = $1`, projectID, env)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EvalRule
	for rows.Next() {
		var r EvalRule
		if err := rows.Scan(&r.FlagKey, &r.Archived, &r.Enabled, &r.RolloutPercent, &r.Conditions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
