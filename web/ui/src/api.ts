import { getActiveChangeSetId } from "./changesetActive";

export type UiConfig = {
  authMode: "dev" | "oidc" | "bootstrap";
  oidcIssuer: string;
  oidcClientId: string;
  oidcAudience: string;
  /** Space-separated OAuth scopes for PKCE login (from KC_OIDC_SCOPES). */
  oidcScopes?: string;
  bootstrapAdminSubject: string;
  uiBasePath: string;
  oidcRedirectPath: string;
};

const DEFAULT_OIDC_SCOPES = "openid profile email groups";
/** Refresh this many ms before access/id token expiry. */
const REFRESH_SKEW_MS = 60_000;

export type AuthSession = {
  mode: UiConfig["authMode"];
  token?: string;
  /** OIDC refresh token (rotated by IdP on each refresh). */
  refreshToken?: string;
  /** Absolute expiry of `token` (ms since epoch), from `expires_in`. */
  expiresAt?: number;
  subject?: string;
  roles?: string;
  displayName?: string;
};

const SESSION_KEY = "kc.session";
/** Same-tab notify when session is written outside React (refresh / expiry). */
export const SESSION_CHANGE_EVENT = "kc.session-change";

type TokenEndpointResponse = {
  id_token?: string;
  access_token?: string;
  refresh_token?: string;
  expires_in?: number;
};

export function loadSession(): AuthSession | null {
  const fromLocal = localStorage.getItem(SESSION_KEY);
  const fromSession = sessionStorage.getItem(SESSION_KEY);
  const raw = fromLocal ?? fromSession;
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as AuthSession;
    // Migrate tab-scoped sessions to localStorage.
    if (!fromLocal && fromSession) {
      localStorage.setItem(SESSION_KEY, raw);
      sessionStorage.removeItem(SESSION_KEY);
    }
    return parsed;
  } catch {
    return null;
  }
}

export function saveSession(s: AuthSession | null) {
  if (!s) localStorage.removeItem(SESSION_KEY);
  else localStorage.setItem(SESSION_KEY, JSON.stringify(s));
  sessionStorage.removeItem(SESSION_KEY);
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent(SESSION_CHANGE_EVENT, { detail: s }));
  }
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function authHeaders(session: AuthSession | null): HeadersInit {
  const h: Record<string, string> = { Accept: "application/json" };
  if (!session) return h;
  if (session.mode === "oidc" || session.mode === "bootstrap") {
    if (session.token) h.Authorization = `Bearer ${session.token}`;
  } else if (session.mode === "dev") {
    if (session.subject) h["X-Subject"] = session.subject;
    if (session.roles) h["X-Roles"] = session.roles;
  }
  return h;
}

function oidcClientId(cfg: UiConfig): string {
  return cfg.oidcClientId || cfg.oidcAudience || "knowledge-core";
}

function applyTokenResponse(base: AuthSession, tokens: TokenEndpointResponse): AuthSession {
  const token = tokens.id_token || tokens.access_token;
  if (!token) throw new Error("no token in response");
  const expiresIn = typeof tokens.expires_in === "number" ? tokens.expires_in : 3600;
  const next: AuthSession = {
    ...base,
    mode: "oidc",
    token,
    expiresAt: Date.now() + expiresIn * 1000,
  };
  if (tokens.refresh_token) next.refreshToken = tokens.refresh_token;
  else if (base.refreshToken) next.refreshToken = base.refreshToken;
  return next;
}

let refreshInFlight: Promise<AuthSession | null> | null = null;

async function oidcDiscovery(issuer: string): Promise<{ token_endpoint: string }> {
  const res = await fetch(`${issuer.replace(/\/$/, "")}/.well-known/openid-configuration`);
  if (!res.ok) throw new Error(`OIDC discovery failed: ${res.status}`);
  return res.json();
}

/** Exchange refresh_token; on failure clears session and returns null. */
export async function refreshOidcSession(
  session: AuthSession,
  cfg?: UiConfig,
): Promise<AuthSession | null> {
  if (session.mode !== "oidc" || !session.refreshToken) {
    return null;
  }
  if (refreshInFlight) return refreshInFlight;

  refreshInFlight = (async () => {
    try {
      const c = cfg ?? (await loadUiConfig());
      if (!c.oidcIssuer) throw new Error("OIDC issuer not configured");
      const discovery = await oidcDiscovery(c.oidcIssuer);
      const body = new URLSearchParams({
        grant_type: "refresh_token",
        client_id: oidcClientId(c),
        refresh_token: session.refreshToken!,
      });
      const tokenRes = await fetch(discovery.token_endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body,
      });
      if (!tokenRes.ok) throw new Error(`refresh failed: ${await tokenRes.text()}`);
      const tokens = (await tokenRes.json()) as TokenEndpointResponse;
      const next = applyTokenResponse(session, tokens);
      saveSession(next);
      return next;
    } catch {
      saveSession(null);
      return null;
    } finally {
      refreshInFlight = null;
    }
  })();

  return refreshInFlight;
}

/** Proactive refresh when within skew of expiry. Legacy sessions without RT pass through. */
export async function ensureFreshSession(
  session: AuthSession | null,
  cfg?: UiConfig,
): Promise<AuthSession | null> {
  if (!session || session.mode !== "oidc") return session;
  const expired =
    typeof session.expiresAt === "number" && Date.now() >= session.expiresAt - REFRESH_SKEW_MS;
  if (!expired) return session;
  if (!session.refreshToken) {
    if (typeof session.expiresAt === "number" && Date.now() >= session.expiresAt) {
      saveSession(null);
      return null;
    }
    return session;
  }
  return refreshOidcSession(session, cfg);
}

async function doFetch(
  path: string,
  init: RequestInit,
  session: AuthSession | null,
): Promise<{ res: Response; body: unknown }> {
  const headers = new Headers(init.headers);
  const auth = authHeaders(session);
  Object.entries(auth).forEach(([k, v]) => headers.set(k, v));
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const method = (init.method || "GET").toUpperCase();
  const csId = getActiveChangeSetId();
  if (csId) {
    const isCsLifecycle =
      path.endsWith("/v1/changesets/open") ||
      /\/v1\/changesets\/[^/]+\/(commit|cancel)$/.test(path) ||
      (method === "POST" && (path === "/v1/changesets" || path.endsWith("/v1/changesets")));
    if (method === "GET" || method === "HEAD") {
      if (!headers.has("X-Knowledge-Changesets")) {
        headers.set("X-Knowledge-Changesets", csId);
      }
    } else if (!isCsLifecycle && !headers.has("X-Knowledge-Changeset")) {
      headers.set("X-Knowledge-Changeset", csId);
    }
  }
  const res = await fetch(path, { ...init, headers });
  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = text;
    }
  }
  return { res, body };
}

function throwIfNotOk(res: Response, body: unknown): void {
  if (res.ok) return;
  const msg =
    typeof body === "object" && body && "error" in body
      ? JSON.stringify((body as { error: unknown }).error)
      : typeof body === "string"
        ? body
        : res.statusText;
  throw new ApiError(res.status, msg || res.statusText);
}

export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {},
  session: AuthSession | null = loadSession(),
  retried = false,
): Promise<T> {
  let active = await ensureFreshSession(session);
  if (session?.mode === "oidc" && session.token && !active?.token) {
    throw new ApiError(401, "session expired");
  }

  const { res, body } = await doFetch(path, init, active);

  if (
    res.status === 401 &&
    !retried &&
    active?.mode === "oidc" &&
    active.refreshToken
  ) {
    const refreshed = await refreshOidcSession(active);
    if (refreshed?.token) {
      return apiFetch(path, init, refreshed, true);
    }
    throw new ApiError(401, "session expired");
  }

  throwIfNotOk(res, body);
  return body as T;
}

export async function loadUiConfig(): Promise<UiConfig> {
  return apiFetch<UiConfig>("/v1/ui/config", {}, null);
}

function b64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let s = "";
  bytes.forEach((b) => {
    s += String.fromCharCode(b);
  });
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export async function startOidcLogin(cfg: UiConfig) {
  const verifier = b64url(crypto.getRandomValues(new Uint8Array(32)).buffer);
  const challenge = b64url(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier)));
  sessionStorage.setItem("kc.pkce.verifier", verifier);
  const state = b64url(crypto.getRandomValues(new Uint8Array(16)).buffer);
  sessionStorage.setItem("kc.oidc.state", state);

  const discovery = await fetch(`${cfg.oidcIssuer.replace(/\/$/, "")}/.well-known/openid-configuration`).then((r) =>
    r.json(),
  );
  const redirectUri = `${window.location.origin}${cfg.oidcRedirectPath}`;
  const params = new URLSearchParams({
    client_id: oidcClientId(cfg),
    response_type: "code",
    scope: (cfg.oidcScopes || "").trim() || DEFAULT_OIDC_SCOPES,
    redirect_uri: redirectUri,
    state,
    code_challenge: challenge,
    code_challenge_method: "S256",
  });
  window.location.href = `${discovery.authorization_endpoint}?${params}`;
}

export async function finishOidcLogin(cfg: UiConfig, code: string, state: string): Promise<AuthSession> {
  const expected = sessionStorage.getItem("kc.oidc.state");
  if (!expected || expected !== state) throw new Error("invalid OIDC state");
  const verifier = sessionStorage.getItem("kc.pkce.verifier");
  if (!verifier) throw new Error("missing PKCE verifier");
  const discovery = await fetch(`${cfg.oidcIssuer.replace(/\/$/, "")}/.well-known/openid-configuration`).then((r) =>
    r.json(),
  );
  const redirectUri = `${window.location.origin}${cfg.oidcRedirectPath}`;
  const body = new URLSearchParams({
    grant_type: "authorization_code",
    client_id: oidcClientId(cfg),
    code,
    redirect_uri: redirectUri,
    code_verifier: verifier,
  });
  const tokenRes = await fetch(discovery.token_endpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!tokenRes.ok) throw new Error(`token exchange failed: ${await tokenRes.text()}`);
  const tokens = (await tokenRes.json()) as TokenEndpointResponse;
  sessionStorage.removeItem("kc.pkce.verifier");
  sessionStorage.removeItem("kc.oidc.state");
  const session = applyTokenResponse({ mode: "oidc" }, tokens);
  const me = await apiFetch<{ id: string; displayName?: string }>("/v1/me", {}, session);
  session.displayName = me.displayName || me.id;
  session.subject = me.id;
  saveSession(session);
  return session;
}
