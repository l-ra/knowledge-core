export type UiConfig = {
  authMode: "dev" | "oidc" | "bootstrap";
  oidcIssuer: string;
  oidcClientId: string;
  oidcAudience: string;
  bootstrapAdminSubject: string;
  uiBasePath: string;
  oidcRedirectPath: string;
};

export type AuthSession = {
  mode: UiConfig["authMode"];
  token?: string;
  subject?: string;
  roles?: string;
};

const SESSION_KEY = "kc.session";

export function loadSession(): AuthSession | null {
  const raw = sessionStorage.getItem(SESSION_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as AuthSession;
  } catch {
    return null;
  }
}

export function saveSession(s: AuthSession | null) {
  if (!s) sessionStorage.removeItem(SESSION_KEY);
  else sessionStorage.setItem(SESSION_KEY, JSON.stringify(s));
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

export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {},
  session: AuthSession | null = loadSession(),
): Promise<T> {
  const headers = new Headers(init.headers);
  const auth = authHeaders(session);
  Object.entries(auth).forEach(([k, v]) => headers.set(k, v));
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
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
  if (!res.ok) {
    const msg =
      typeof body === "object" && body && "error" in body
        ? JSON.stringify((body as { error: unknown }).error)
        : text || res.statusText;
    throw new ApiError(res.status, msg);
  }
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
    client_id: cfg.oidcClientId || cfg.oidcAudience || "knowledge-core",
    response_type: "code",
    scope: "openid profile email",
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
    client_id: cfg.oidcClientId || cfg.oidcAudience || "knowledge-core",
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
  const tokens = await tokenRes.json();
  const token = tokens.id_token || tokens.access_token;
  if (!token) throw new Error("no token in response");
  sessionStorage.removeItem("kc.pkce.verifier");
  sessionStorage.removeItem("kc.oidc.state");
  const session: AuthSession = { mode: "oidc", token };
  saveSession(session);
  return session;
}
