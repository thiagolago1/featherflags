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

  /**
   * Subscribe to the server's SSE stream. `onChange` fires whenever any flag
   * in this environment's project is updated — callers should re-evaluate.
   * Reconnects automatically with backoff. Returns an unsubscribe function.
   *
   * Uses XMLHttpRequest incremental reads (works on React Native, where fetch
   * bodies can't stream) and falls back to fetch streaming elsewhere.
   */
  subscribe(onChange: () => void): () => void {
    let stopped = false;
    let attempt = 0;
    let abort: (() => void) | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    const url = `${this.baseUrl}/v1/stream?apiKey=${encodeURIComponent(this.apiKey)}`;

    const scheduleReconnect = () => {
      if (stopped) return;
      const delay = Math.min(1000 * 2 ** attempt, 30_000);
      attempt += 1;
      retryTimer = setTimeout(connect, delay);
    };

    // Every complete SSE frame ends in a blank line; we only care whether a
    // "change" event is in the newly received chunk.
    const handleChunk = (chunk: string) => {
      if (chunk.includes("event: change")) {
        attempt = 0; // healthy connection
        onChange();
      }
    };

    const connect = () => {
      if (stopped) return;

      if (typeof XMLHttpRequest !== "undefined") {
        const xhr = new XMLHttpRequest();
        let seen = 0;
        xhr.open("GET", url);
        xhr.setRequestHeader("Accept", "text/event-stream");
        xhr.onprogress = () => {
          handleChunk(xhr.responseText.slice(seen));
          seen = xhr.responseText.length;
        };
        xhr.onerror = scheduleReconnect;
        xhr.onload = scheduleReconnect; // server closed: reconnect
        xhr.send();
        abort = () => xhr.abort();
        return;
      }

      // Node / environments with streaming fetch
      const controller = new AbortController();
      abort = () => controller.abort();
      fetch(url, { signal: controller.signal, headers: { Accept: "text/event-stream" } })
        .then(async (res) => {
          if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`);
          const reader = res.body.getReader();
          const decoder = new TextDecoder();
          for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            handleChunk(decoder.decode(value, { stream: true }));
          }
          scheduleReconnect();
        })
        .catch(() => {
          if (!stopped) scheduleReconnect();
        });
    };

    connect();
    return () => {
      stopped = true;
      clearTimeout(retryTimer);
      abort?.();
    };
  }
}
