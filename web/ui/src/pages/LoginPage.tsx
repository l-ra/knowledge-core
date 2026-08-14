import { FormEvent, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../auth";
import { apiFetch, startOidcLogin } from "../api";

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { cfg, ready, session, setSession } = useAuth();
  const [password, setPassword] = useState("");
  const [subject, setSubject] = useState("admin");
  const [roles, setRoles] = useState("admin");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  if (!ready) return <div className="login-page">{t("login.loading")}</div>;
  if (session) return <Navigate to="/" replace />;
  if (!cfg) return <div className="login-page error">{t("common.error")}: config</div>;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (cfg!.authMode === "bootstrap") {
        const next = { mode: "bootstrap" as const, token: password };
        const me = await apiFetch<{ id: string; displayName?: string }>("/v1/me", {}, next);
        setSession({ ...next, subject: me.id, displayName: me.displayName || me.id });
        navigate("/", { replace: true });
      } else if (cfg!.authMode === "dev") {
        const next = { mode: "dev" as const, subject, roles };
        const me = await apiFetch<{ id: string; displayName?: string }>("/v1/me", {}, next);
        setSession({ ...next, displayName: me.displayName || me.id || subject });
        navigate("/", { replace: true });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("login.error"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <div>
          <h1>{t("login.title")}</h1>
          <p className="muted">{t("login.subtitle")}</p>
        </div>

        {cfg.authMode === "oidc" && (
          <button
            type="button"
            className="primary"
            disabled={busy || !cfg.oidcIssuer}
            onClick={() => void startOidcLogin(cfg)}
          >
            {t("login.oidc")}
          </button>
        )}

        {cfg.authMode === "bootstrap" && (
          <label className="field">
            {t("login.bootstrap")}
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </label>
        )}

        {cfg.authMode === "dev" && (
          <>
            <label className="field">
              {t("login.devSubject")}
              <input value={subject} onChange={(e) => setSubject(e.target.value)} required />
            </label>
            <label className="field">
              {t("login.devRoles")}
              <input value={roles} onChange={(e) => setRoles(e.target.value)} />
            </label>
          </>
        )}

        {cfg.authMode !== "oidc" && (
          <button className="primary" type="submit" disabled={busy}>
            {t("login.submit")}
          </button>
        )}

        {error && <p className="error">{error}</p>}
      </form>
    </div>
  );
}
