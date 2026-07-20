export type Env = "development" | "staging" | "production";
export const ENVS: Env[] = ["development", "staging", "production"];

export interface Condition {
  attr: string;
  op: "eq" | "neq" | "in" | "semver_gte";
  value: unknown;
}

export interface FlagRule {
  env: Env;
  enabled: boolean;
  rolloutPercent: number;
  conditions: Condition[] | null;
}

export interface Flag {
  id: string;
  key: string;
  description: string | null;
  archived: boolean;
  rules: FlagRule[];
}

export interface Project {
  id: string;
  name: string;
  apiKeys: { key: string; env: Env }[];
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

// Same-origin only: this hits the Next.js BFF proxy (src/app/api/backend),
// which is the only thing that knows the real API's URL and service token.
// The session cookie (httpOnly, set by next-auth) rides along automatically.
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/backend/${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) msg = body.error;
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  listProjects: () => request<Project[]>("admin/projects"),

  createProject: (name: string) =>
    request<Project>("admin/projects", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),

  listFlags: (projectId: string) => request<Flag[]>(`admin/projects/${projectId}/flags`),

  createFlag: (projectId: string, key: string, description?: string) =>
    request<Flag>(`admin/projects/${projectId}/flags`, {
      method: "POST",
      body: JSON.stringify({ key, description }),
    }),

  updateRule: (
    flagId: string,
    env: Env,
    patch: { enabled?: boolean; rolloutPercent?: number; conditions?: Condition[] | null },
  ) =>
    request<void>(`admin/flags/${flagId}/rules/${env}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  setArchived: (flagId: string, archived: boolean) =>
    request<void>(`admin/flags/${flagId}/${archived ? "archive" : "unarchive"}`, {
      method: "POST",
      body: "{}",
    }),
};
