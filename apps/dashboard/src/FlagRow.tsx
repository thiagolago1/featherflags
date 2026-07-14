import { useEffect, useRef, useState } from "react";
import { api, ENVS, type Condition, type Env, type Flag, type FlagRule } from "./api";

export function FlagRow({
  flag,
  token,
  onError,
  onChanged,
}: {
  flag: Flag;
  token: string;
  onError: (err: unknown) => void;
  onChanged: () => Promise<void>;
}) {
  const [editing, setEditing] = useState<Env | null>(null);
  const [busyEnv, setBusyEnv] = useState<Env | null>(null);

  async function toggle(rule: FlagRule) {
    // Production asks; the other environments just flip.
    if (
      rule.env === "production" &&
      !window.confirm(
        `${rule.enabled ? "Disable" : "Enable"} "${flag.key}" in PRODUCTION?`,
      )
    ) {
      return;
    }
    setBusyEnv(rule.env);
    try {
      await api.updateRule(token, flag.id, rule.env, { enabled: !rule.enabled });
      await onChanged();
    } catch (err) {
      onError(err);
    } finally {
      setBusyEnv(null);
    }
  }

  async function saveRule(env: Env, rolloutPercent: number, conditions: Condition[] | null) {
    try {
      await api.updateRule(token, flag.id, env, { rolloutPercent, conditions });
      await onChanged();
      setEditing(null);
    } catch (err) {
      onError(err);
    }
  }

  async function toggleArchived() {
    try {
      await api.setArchived(token, flag.id, !flag.archived);
      await onChanged();
    } catch (err) {
      onError(err);
    }
  }

  return (
    <div className={`flag-row${flag.archived ? " archived" : ""}`}>
      <div className="flag-name">
        <div className="key mono">{flag.key}</div>
        {flag.description && <div className="desc">{flag.description}</div>}
      </div>

      {ENVS.map((env) => {
        const rule = flag.rules.find((r) => r.env === env);
        if (!rule) return <div key={env} />;
        const partial = rule.rolloutPercent < 100;
        const hasConds = (rule.conditions?.length ?? 0) > 0;
        return (
          <div key={env} className="env-cell">
            <button
              role="switch"
              aria-checked={rule.enabled}
              aria-label={`${flag.key} in ${env}`}
              className="switch"
              data-env={env}
              disabled={flag.archived || busyEnv === env}
              onClick={() => void toggle(rule)}
            />
            <button
              className={`rollout-btn${partial ? " partial" : ""}${hasConds ? " conditioned" : ""}`}
              onClick={() => setEditing(env)}
              disabled={flag.archived}
              aria-label={`Edit ${env} rule for ${flag.key}`}
            >
              {rule.rolloutPercent}%
            </button>
            {editing === env && (
              <RuleEditor
                env={env}
                rule={rule}
                onCancel={() => setEditing(null)}
                onSave={saveRule}
              />
            )}
          </div>
        );
      })}

      <button
        className="row-menu-btn"
        title={flag.archived ? "Unarchive flag" : "Archive flag"}
        aria-label={flag.archived ? `Unarchive ${flag.key}` : `Archive ${flag.key}`}
        onClick={() => void toggleArchived()}
      >
        {flag.archived ? (
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
            <path d="M3 9l4-6h10l4 6M3 9v11a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1V9M3 9h18M12 19v-6m0 0l-3 3m3-3l3 3" />
          </svg>
        ) : (
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
            <path d="M21 8v13H3V8M1 3h22v5H1zM10 12h4" />
          </svg>
        )}
      </button>
    </div>
  );
}

function RuleEditor({
  env,
  rule,
  onSave,
  onCancel,
}: {
  env: Env;
  rule: FlagRule;
  onSave: (env: Env, rolloutPercent: number, conditions: Condition[] | null) => Promise<void>;
  onCancel: () => void;
}) {
  const [percent, setPercent] = useState(rule.rolloutPercent);
  const [condsText, setCondsText] = useState(
    rule.conditions?.length ? JSON.stringify(rule.conditions, null, 2) : "",
  );
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    ref.current?.querySelector("input")?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onCancel();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel]);

  function parseConditions(): Condition[] | null | undefined {
    const text = condsText.trim();
    if (!text) return null;
    try {
      const parsed = JSON.parse(text) as unknown;
      if (!Array.isArray(parsed)) throw new Error("must be a JSON array");
      for (const c of parsed as Condition[]) {
        if (!c.attr || !["eq", "neq", "in"].includes(c.op)) {
          throw new Error('each item needs "attr" and "op" (eq | neq | in)');
        }
      }
      return parsed as Condition[];
    } catch (e) {
      setError(e instanceof Error ? e.message : "invalid JSON");
      return undefined;
    }
  }

  async function save() {
    setError(null);
    const conds = parseConditions();
    if (conds === undefined) return;
    setBusy(true);
    await onSave(env, percent, conds);
    setBusy(false);
  }

  return (
    <>
      <div className="popover-backdrop" onClick={onCancel} />
      <div className="rule-editor" ref={ref} role="dialog" aria-label={`${env} rule`}>
        <h3>
          <span className="env-dot" data-env={env} /> {env}
        </h3>
        <div>
          <label htmlFor={`rollout-${env}`}>Rollout</label>
          <div className="rollout-field">
            <input
              id={`rollout-${env}`}
              type="range"
              min={0}
              max={100}
              step={5}
              value={percent}
              onChange={(e) => setPercent(Number(e.target.value))}
            />
            <output>{percent}%</output>
          </div>
        </div>
        <div>
          <label htmlFor={`conds-${env}`}>
            Conditions <span style={{ opacity: 0.7 }}>(JSON, empty = everyone)</span>
          </label>
          <textarea
            id={`conds-${env}`}
            value={condsText}
            onChange={(e) => setCondsText(e.target.value)}
            placeholder={'[{"attr":"plan","op":"eq","value":"premium"}]'}
            spellCheck={false}
          />
          {error && <div className="error">{error}</div>}
        </div>
        <div className="rule-actions">
          <button className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button className="btn btn-primary" onClick={() => void save()} disabled={busy}>
            Save
          </button>
        </div>
      </div>
    </>
  );
}
