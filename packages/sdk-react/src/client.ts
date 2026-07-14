export type FlagMap = Record<string, boolean>;

export interface FlagUser {
  id: string;
  attributes?: Record<string, string>;
}

/**
 * Pluggable persistence so the SDK works anywhere:
 * - React Native: pass AsyncStorage
 * - Web: pass window.localStorage (sync APIs are accepted)
 * - Tests / none: omit — flags just won't survive a restart
 */
export interface FlagStorage {
  getItem(key: string): Promise<string | null> | string | null;
  setItem(key: string, value: string): Promise<void> | void;
}

export interface ClientOptions {
  /** featherflags server URL, e.g. https://flags.example.com */
  baseUrl: string;
  /** Environment API key (ff_dev_* / ff_stg_* / ff_prod_*) */
  apiKey: string;
  storage?: FlagStorage;
  /** Request timeout in ms (default 5000) */
  timeoutMs?: number;
}

/**
 * Fail-safe evaluation client. The contract: this never throws and the app
 * never breaks because the flags service is down — worst case every flag
 * evaluates to `false` (or the last cached value).
 */
export class FeatherflagsClient {
  private readonly baseUrl: string;
  private readonly apiKey: string;
  private readonly storage?: FlagStorage;
  private readonly timeoutMs: number;

  constructor(opts: ClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, "");
    this.apiKey = opts.apiKey;
    this.storage = opts.storage;
    this.timeoutMs = opts.timeoutMs ?? 5000;
  }

  private cacheKey(userId: string): string {
    // Keyed by API key + user so switching environment or account never
    // serves another context's flags.
    return `featherflags:${this.apiKey}:${userId}`;
  }

  /** Last successful response for this user, if any. */
  async loadCached(user: FlagUser): Promise<FlagMap | null> {
    if (!this.storage) return null;
    try {
      const raw = await this.storage.getItem(this.cacheKey(user.id));
      return raw ? (JSON.parse(raw) as FlagMap) : null;
    } catch {
      return null;
    }
  }

  /**
   * Fetch fresh flags. On any failure (network, 5xx, bad JSON) falls back to
   * the cached map, then to `{}`.
   */
  async evaluate(user: FlagUser): Promise<FlagMap> {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), this.timeoutMs);
      const res = await fetch(`${this.baseUrl}/v1/evaluate`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-API-Key": this.apiKey,
        },
        body: JSON.stringify({ userId: user.id, attributes: user.attributes ?? {} }),
        signal: controller.signal,
      });
      clearTimeout(timer);
      if (!res.ok) throw new Error(`featherflags: HTTP ${res.status}`);
      const body = (await res.json()) as { flags?: FlagMap };
      const flags = body.flags ?? {};
      if (this.storage) {
        try {
          await this.storage.setItem(this.cacheKey(user.id), JSON.stringify(flags));
        } catch {
          // Cache write failures are non-fatal.
        }
      }
      return flags;
    } catch {
      return (await this.loadCached(user)) ?? {};
    }
  }
}
