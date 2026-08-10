import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

export function EntityHistoryPage() {
  const { qid = "" } = useParams();
  const { t } = useTranslation();
  const [revs, setRevs] = useState<unknown[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    apiFetch<{ revisions: unknown[] }>(`/v1/entities/${qid}/history`)
      .then((r) => setRevs(r.revisions || []))
      .catch((err) => setError(err instanceof Error ? err.message : t("common.error")));
  }, [qid, t]);

  return (
    <div className="stack">
      <p>
        <Link to={`/entities/${qid}`}>{qid}</Link> / {t("entity.history")}
      </p>
      <h1>{t("entity.history")}</h1>
      {error && <p className="error">{error}</p>}
      <pre>{JSON.stringify(revs, null, 2)}</pre>
    </div>
  );
}
