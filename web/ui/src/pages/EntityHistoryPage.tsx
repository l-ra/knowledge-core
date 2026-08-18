import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { EntityLink } from "../links";
import { useEntityLookup } from "../useEntityLookup";

export function EntityHistoryPage() {
  const { qid: rawQid = "" } = useParams();
  const qid = (() => {
    try {
      return decodeURIComponent(rawQid);
    } catch {
      return rawQid;
    }
  })();
  const { t, i18n } = useTranslation();
  const [revs, setRevs] = useState<unknown[]>([]);
  const [error, setError] = useState("");
  const entities = useEntityLookup([qid]);

  useEffect(() => {
    apiFetch<{ revisions: unknown[] }>(`/v1/entities/${encodeURIComponent(qid)}/history`)
      .then((r) => setRevs(r.revisions || []))
      .catch((err) => setError(err instanceof Error ? err.message : t("common.error")));
  }, [qid, t]);

  return (
    <div className="stack">
      <nav className="breadcrumb muted">
        <Link to="/entities">{t("nav.entities")}</Link>
        {" / "}
        <EntityLink id={qid} displayId={entities[qid]?.displayId} labels={entities[qid]?.labels} lang={i18n.language} />
        {" / "}
        {t("entity.history")}
      </nav>
      <h1>{t("entity.history")}</h1>
      {error && <p className="error">{error}</p>}
      <pre>{JSON.stringify(revs, null, 2)}</pre>
    </div>
  );
}
