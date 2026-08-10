import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type AuthAdmin = {
  authMode: string;
  oidcIssuer: string;
  oidcClientId: string;
  oidcAudience: string;
  source: string;
  updatedAt?: string;
  updatedBy?: string;
};

export function AuthPage() {
  const { t } = useTranslation();
  const [data, setData] = useState<AuthAdmin | null>(null);
  const [issuer, setIssuer] = useState("");
  const [clientId, setClientId] = useState("");
  const [audience, setAudience] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function reload() {
    setError("");
    try {
      const res = await apiFetch<AuthAdmin>("/v1/admin/auth");
      setData(res);
      setIssuer(res.oidcIssuer || "http://pocket-id.localhost:1411");
      setClientId(res.oidcClientId || "");
      setAudience(res.oidcAudience || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  async function onSave(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await apiFetch<AuthAdmin>("/v1/admin/auth", {
        method: "PUT",
        body: JSON.stringify({
          authMode: "oidc",
          oidcIssuer: issuer.trim(),
          oidcClientId: clientId.trim(),
          oidcAudience: audience.trim(),
        }),
      });
      setData(res);
      sessionStorage.removeItem("kc.session");
      window.location.assign(`${import.meta.env.BASE_URL}login`);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
      setBusy(false);
    }
  }

  if (!data && !error) return <p>{t("common.loading")}</p>;

  const origin = typeof window !== "undefined" ? window.location.origin : "http://localhost:8080";
  const callback = `${origin}/ui/callback`;

  return (
    <div className="stack">
      <h1>{t("auth.title")}</h1>
      <p className="muted">{t("auth.subtitle")}</p>
      {error && <p className="error">{error}</p>}

      <section className="panel stack">
        <h2>{t("auth.status")}</h2>
        <p>
          <strong>{t("auth.mode")}:</strong> {data?.authMode}{" "}
          <span className="muted">({data?.source})</span>
        </p>
        {data?.updatedAt && (
          <p className="muted">
            {t("auth.updated")}: {data.updatedBy} · {data.updatedAt}
          </p>
        )}
      </section>

      <section className="panel stack">
        <h2>{t("auth.idpTitle")}</h2>
        <ol className="steps">
          <li>{t("auth.idpStep1")}</li>
          <li>{t("auth.idpStep2")}</li>
          <li>{t("auth.idpStep3")}</li>
          <li>{t("auth.idpStep4")}</li>
        </ol>
        <p>
          <strong>{t("auth.callback")}:</strong> <code>{callback}</code>
        </p>
      </section>

      <section className="panel stack">
        <h2>{t("auth.kcTitle")}</h2>
        <ol className="steps">
          <li>{t("auth.kcStep1")}</li>
          <li>{t("auth.kcStep2")}</li>
          <li>{t("auth.kcStep3")}</li>
          <li>{t("auth.kcStep4")}</li>
        </ol>
        <form className="stack" onSubmit={onSave}>
          <label className="field">
            {t("auth.issuer")}
            <input
              value={issuer}
              onChange={(e) => setIssuer(e.target.value)}
              required
              placeholder="http://pocket-id.localhost:1411"
            />
          </label>
          <label className="field">
            {t("auth.clientId")}
            <input
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
              required
              placeholder="id from Pocket ID"
            />
          </label>
          <label className="field">
            {t("auth.audience")}
            <input
              value={audience}
              onChange={(e) => setAudience(e.target.value)}
              placeholder={t("auth.audienceHint")}
            />
          </label>
          <button className="primary" type="submit" disabled={busy}>
            {t("auth.enable")}
          </button>
        </form>
      </section>
    </div>
  );
}
