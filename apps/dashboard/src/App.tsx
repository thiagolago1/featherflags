import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, ENVS, type Env, type Flag, type Project } from "./api";
import { FlagRow } from "./FlagRow";

const TOKEN_KEY = "featherflags.adminToken";

export default function App() {
  const [token, setToken] = useState<string | null>(() => sessionStorage.getItem(TOKEN_KEY));
  if (!token) {
    return (
      <TokenGate
        onSubmit={(t) => {
          sessionStorage.setItem(TOKEN_KEY, t);
          setToken(t);
        }}
      />
    );
  }
  return (
    <Dashboard
      token={token}
      onAuthError={() => {
        sessionStorage.removeItem(TOKEN_KEY);
        setToken(null);
      }}
    />
  );
}

function TokenGate({ onSubmit }: { onSubmit: (token: string) => void }) {
  const [value, setValue] = useState("");
  return (
    <div className="gate">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (value.trim()) onSubmit(value.trim());
        }}
      >
        <div className="wordmark">
          <span className="feather">🪶</span> featherflags
        </div>
        <p>Paste the server&apos;s admin token to open the dashboard.</p>
        <input
          type="password"
          placeholder="Admin token"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          autoFocus
        />
        <button className="btn btn-primary" type="submit" disabled={!value.trim()}>
          Open dashboard
        </button>
      </form>
    </div>
  );
}

function Dashboard({ token, onAuthError }: { token: string; onAuthError: () => void }) {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [flags, setFlags] = useState<Flag[] | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout>>();

  const fail = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && err.status === 401) {
        onAuthError();
        return;
      }
      clearTimeout(toastTimer.current);
      setToast(err instanceof Error ? err.message : "Something went wrong");
      toastTimer.current = setTimeout(() => setToast(null), 4000);
    },
    [onAuthError],
  );

  const loadProjects = useCallback(async () => {
    try {
      const ps = await api.listProjects(token);
      setProjects(ps);
      setSelectedId((cur) => cur ?? ps[0]?.id ?? null);
    } catch (err) {
      fail(err);
    }
  }, [token, fail]);

  useEffect(() => {
    void loadProjects();
  }, [loadProjects]);

  const loadFlags = useCallback(async () => {
    if (!selectedId) return;
    try {
      setFlags(await api.listFlags(token, selectedId));
    } catch (err) {
      fail(err);
    }
  }, [token, selectedId, fail]);

  useEffect(() => {
    setFlags(null);
    void loadFlags();
  }, [loadFlags]);

  const selected = projects?.find((p) => p.id === selectedId) ?? null;

  async function createProject(name: string) {
    try {
      const p = await api.createProject(token, name);
      setProjects((ps) => [...(ps ?? []), p]);
      setSelectedId(p.id);
    } catch (err) {
      fail(err);
    }
  }

  async function createFlag(key: string) {
    if (!selectedId) return;
    try {
      const f = await api.createFlag(token, selectedId, key);
      setFlags((fs) => [...(fs ?? []), f]);
    } catch (err) {
      fail(err);
    }
  }

  return (
    <div className="shell">
      <nav className="sidebar">
        <div className="wordmark">
          <span className="feather">🪶</span> featherflags
        </div>
        <div className="sidebar-label">Projects</div>
        {projects?.map((p) => (
          <button
            key={p.id}
            className="project-btn"
            aria-current={p.id === selectedId}
            onClick={() => setSelectedId(p.id)}
          >
            {p.name}
          </button>
        ))}
        {projects && <NewItemForm placeholder="New project…" onCreate={createProject} />}
      </nav>

      <main className="main">
        {selected ? (
          <>
            <div className="main-head">
              <h1>{selected.name}</h1>
            </div>
            <ApiKeys project={selected} />
            <FlagsTable
              flags={flags}
              token={token}
              onError={fail}
              onChanged={loadFlags}
              onCreate={createFlag}
            />
          </>
        ) : projects && projects.length === 0 ? (
          <div className="empty">Create your first project in the sidebar to get started.</div>
        ) : null}
      </main>

      {toast && (
        <div className="toast" role="alert">
          {toast}
        </div>
      )}
    </div>
  );
}

function ApiKeys({ project }: { project: Project }) {
  const [copied, setCopied] = useState<Env | null>(null);
  return (
    <div className="keys-row">
      {ENVS.map((env) => {
        const key = project.apiKeys.find((k) => k.env === env);
        if (!key) return null;
        return (
          <button
            key={env}
            className="key-chip mono"
            title={`Copy ${env} API key`}
            onClick={async () => {
              await navigator.clipboard.writeText(key.key);
              setCopied(env);
              setTimeout(() => setCopied(null), 1200);
            }}
          >
            <span className="env-dot" data-env={env} />
            {copied === env ? "copied!" : key.key}
          </button>
        );
      })}
    </div>
  );
}

function FlagsTable({
  flags,
  token,
  onError,
  onChanged,
  onCreate,
}: {
  flags: Flag[] | null;
  token: string;
  onError: (err: unknown) => void;
  onChanged: () => Promise<void>;
  onCreate: (key: string) => Promise<void>;
}) {
  if (!flags) return null;
  return (
    <>
      <div className="flags-table">
        <div className="flags-header">
          <div>Flag</div>
          {ENVS.map((env) => (
            <div key={env} className="env-col" data-env={env}>
              <span className="env-dot" data-env={env} />
              {env}
            </div>
          ))}
          <div />
        </div>
        {flags.length === 0 ? (
          <div className="empty">
            No flags yet — add one below, then read it in the app with{" "}
            <code>useFlag(&quot;my-flag&quot;)</code>.
          </div>
        ) : (
          flags.map((f) => (
            <FlagRow key={f.id} flag={f} token={token} onError={onError} onChanged={onChanged} />
          ))
        )}
      </div>
      <NewItemForm className="new-flag" placeholder="new-flag-key" mono onCreate={onCreate} />
    </>
  );
}

function NewItemForm({
  placeholder,
  onCreate,
  className,
  mono,
}: {
  placeholder: string;
  onCreate: (value: string) => Promise<void> | void;
  className?: string;
  mono?: boolean;
}) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const v = value.trim();
    if (!v || busy) return;
    setBusy(true);
    await onCreate(v);
    setBusy(false);
    setValue("");
  }

  return (
    <form className={className} onSubmit={submit}>
      <input
        className={mono ? "mono" : undefined}
        placeholder={placeholder}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        aria-label={placeholder}
      />
      <button className="btn" type="submit" disabled={!value.trim() || busy}>
        Add
      </button>
    </form>
  );
}
