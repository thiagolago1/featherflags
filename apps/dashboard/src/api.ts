export type Env = "development" | "staging" | "production";
export const ENVS: Env[] = ["development", "staging", "production"];

export interface Condition {
  attr: string;
  op: "eq" | "neq" | "in";
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

/** Same-origin in dev (vite proxy); VITE_API_URL for a deployed dashboard. */
const BASE = (import.meta.env.VITE_API_URL as string | undefined)?.replace(/\/+$/, "") ?? "";

async function request<T>(token: string, path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      ...init?.headers,
    },
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
  listProjects: (token: string) => request<Project[]>(token, "/admin/projects"),

  createProject: (token: string, name: string) =>
    request<Project>(token, "/admin/projects", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),

  listFlags: (token: string, projectId: string) =>
    request<Flag[]>(token, `/admin/projects/${projectId}/flags`),

  createFlag: (token: string, projectId: string, key: string, description?: string) =>
    request<Flag>(token, `/admin/projects/${projectId}/flags`, {
      method: "POST",
      body: JSON.stringify({ key, description }),
    }),

  updateRule: (
    token: string,
    flagId: string,
    env: Env,
    patch: { enabled?: boolean; rolloutPercent?: number; conditions?: Condition[] | null },
  ) =>
    request<void>(token, `/admin/flags/${flagId}/rules/${env}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  setArchived: (token: string, flagId: string, archived: boolean) =>
    request<void>(token, `/admin/flags/${flagId}/${archived ? "archive" : "unarchive"}`, {
      method: "POST",
      body: "{}",
    }),
};
