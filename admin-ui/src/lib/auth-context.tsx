import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { GlyphuxApiError, type User } from "@glyphux/sdk";
import { client, setToken } from "./client";

interface AuthState {
  /** The authenticated user, or undefined while unauthenticated. */
  user: User | undefined;
  /** True until the initial session check (client.auth.me()) resolves —
   * lets callers avoid a login-page flash for a user who already has a
   * valid persisted token. */
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthState | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | undefined>(undefined);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!client.token) {
        setLoading(false);
        return;
      }
      try {
        const me = await client.auth.me();
        if (!cancelled) setUser(me);
      } catch (err) {
        // Only a genuine auth rejection (401) means the token itself is
        // stale/expired — drop it and fall through to the login page. A
        // transient 5xx/network failure must not wipe an otherwise-valid
        // token; that would force an unnecessary re-login on every blip.
        if (err instanceof GlyphuxApiError && err.status === 401) setToken(undefined);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const result = await client.auth.login(email, password);
    setToken(result.token);
    setUser({ id: result.id, email: result.email, role: result.role });
  }, []);

  const logout = useCallback(async () => {
    try {
      await client.auth.logout();
    } finally {
      setToken(undefined);
      setUser(undefined);
    }
  }, []);

  return <AuthContext.Provider value={{ user, loading, login, logout }}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
}
