import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { FeatherflagsClient, type FlagMap, type FlagStorage, type FlagUser } from "./client";

interface FlagsContextValue {
  flags: FlagMap;
  isLoading: boolean;
  refresh: () => Promise<void>;
}

const FlagsContext = createContext<FlagsContextValue>({
  flags: {},
  isLoading: false,
  refresh: async () => {},
});

export interface FlagsProviderProps {
  baseUrl: string;
  apiKey: string;
  user: FlagUser;
  /** e.g. AsyncStorage on React Native, localStorage on web */
  storage?: FlagStorage;
  /** Re-fetch interval in ms. 0 disables polling (default 60000). */
  pollIntervalMs?: number;
  children: ReactNode;
}

export function FlagsProvider({
  baseUrl,
  apiKey,
  user,
  storage,
  pollIntervalMs = 60_000,
  children,
}: FlagsProviderProps) {
  const client = useMemo(
    () => new FeatherflagsClient({ baseUrl, apiKey, storage }),
    [baseUrl, apiKey, storage],
  );
  const [flags, setFlags] = useState<FlagMap>({});
  const [isLoading, setIsLoading] = useState(true);

  // Serialize user so attribute-object identity churn doesn't refetch forever.
  const userKey = JSON.stringify([user.id, user.attributes ?? {}]);
  const userRef = useRef(user);
  userRef.current = user;

  const refresh = useCallback(async () => {
    const fresh = await client.evaluate(userRef.current);
    setFlags(fresh);
    setIsLoading(false);
  }, [client]);

  useEffect(() => {
    let cancelled = false;

    // Hydrate from cache first so the UI has flags on the very first frame
    // after a cold start, then revalidate over the network.
    (async () => {
      const cached = await client.loadCached(userRef.current);
      if (!cancelled && cached) {
        setFlags(cached);
        setIsLoading(false);
      }
      await refresh();
    })();

    const interval =
      pollIntervalMs > 0 ? setInterval(() => void refresh(), pollIntervalMs) : undefined;
    return () => {
      cancelled = true;
      if (interval) clearInterval(interval);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, refresh, pollIntervalMs, userKey]);

  const value = useMemo(() => ({ flags, isLoading, refresh }), [flags, isLoading, refresh]);
  return <FlagsContext.Provider value={value}>{children}</FlagsContext.Provider>;
}

/** All flags plus loading state and a manual refresh. */
export function useFlags(): FlagsContextValue {
  return useContext(FlagsContext);
}

/** A single flag. Unknown flags (or service down with no cache) are `false`. */
export function useFlag(key: string, defaultValue = false): boolean {
  const { flags } = useFlags();
  return flags[key] ?? defaultValue;
}
