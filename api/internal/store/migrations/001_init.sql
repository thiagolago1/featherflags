CREATE TYPE environment AS ENUM ('development', 'staging', 'production');

CREATE TABLE projects (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
    id         TEXT PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    env        environment NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, env)
);

CREATE TABLE flags (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL,
    description TEXT,
    archived    BOOLEAN NOT NULL DEFAULT false,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, key)
);

CREATE TABLE flag_rules (
    id              TEXT PRIMARY KEY,
    flag_id         TEXT NOT NULL REFERENCES flags(id) ON DELETE CASCADE,
    env             environment NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT false,
    rollout_percent INT NOT NULL DEFAULT 100 CHECK (rollout_percent BETWEEN 0 AND 100),
    conditions      JSONB,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (flag_id, env)
);

CREATE INDEX idx_flags_project ON flags(project_id) WHERE NOT archived;
