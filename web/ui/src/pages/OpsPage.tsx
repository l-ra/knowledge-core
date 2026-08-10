import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

export function OpsPage() {
  const { t } = useTranslation();
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  async function run(path: string, label: string) {
    setError("");
    setMsg("");
    try {
      const res = await apiFetch<unknown>(path, { method: "POST" });
      setMsg(`${label}: ${JSON.stringify(res)}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("ops.title")}</h1>
      <div className="row">
        <button type="button" className="primary" onClick={() => void run("/v1/projections/outbox/process", "outbox")}>
          {t("ops.outbox")}
        </button>
        <button type="button" onClick={() => void run("/v1/projections/search/rebuild", "search")}>
          {t("ops.rebuildSearch")}
        </button>
        <button type="button" onClick={() => void run("/v1/projections/rdf/rebuild", "rdf")}>
          {t("ops.rebuildRdf")}
        </button>
      </div>
      {msg && <p className="muted">{msg}</p>}
      {error && <p className="error">{error}</p>}
    </div>
  );
}
