import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../auth";
import { finishOidcLogin } from "../api";

export function OidcCallbackPage() {
  const { t } = useTranslation();
  const { cfg, setSession } = useAuth();
  const [params] = useSearchParams();
  const nav = useNavigate();
  const [error, setError] = useState("");

  useEffect(() => {
    if (!cfg) return;
    const code = params.get("code");
    const state = params.get("state");
    if (!code || !state) {
      setError("missing code/state");
      return;
    }
    finishOidcLogin(cfg, code, state)
      .then((session) => {
        setSession(session);
        nav("/search", { replace: true });
      })
      .catch((e) => setError(e instanceof Error ? e.message : t("login.error")));
  }, [cfg, params, setSession, nav, t]);

  return (
    <div className="login-page">
      <div className="login-card">
        <h1>{t("login.oidc")}</h1>
        {error ? <p className="error">{error}</p> : <p className="muted">{t("common.loading")}</p>}
      </div>
    </div>
  );
}
