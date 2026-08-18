import { FormEvent, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { EntityLink, StatementLink } from "../links";
import { useEntityLookup } from "../useEntityLookup";

type Hit = {
  objectType: string;
  publicId: string;
  subjectQid?: string;
  propertyPid?: string;
  searchText: string;
};

export function SearchPage() {
  const { t, i18n } = useTranslation();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<Hit[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const entityIds = useMemo(
    () =>
      hits
        .filter((h) => h.objectType === "entity" || h.objectType === "property" || h.objectType === "class")
        .map((h) => h.publicId)
        .concat(hits.filter((h) => h.subjectQid).map((h) => h.subjectQid!)),
    [hits],
  );
  const entities = useEntityLookup(entityIds);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await apiFetch<{ results: Hit[] }>(`/v1/projections/search?q=${encodeURIComponent(q)}`);
      setHits(res.results || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <h1>{t("nav.search")}</h1>
      <form className="toolbar" onSubmit={onSubmit}>
        <input
          style={{ flex: 1, minWidth: "12rem" }}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t("search.placeholder")}
        />
        <button className="primary" disabled={busy || !q.trim()}>
          {t("search.submit")}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
      {!busy && hits.length === 0 && q && <p className="muted">{t("search.empty")}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>Type</th>
            <th>ID</th>
            <th>Text</th>
          </tr>
        </thead>
        <tbody>
          {hits.map((h) => (
            <tr key={`${h.objectType}:${h.publicId}`}>
              <td>{h.objectType}</td>
              <td>
                {h.objectType === "entity" || h.objectType === "property" || h.objectType === "class" ? (
                  <EntityLink id={h.publicId} labels={entities[h.publicId]?.labels} lang={i18n.language} />
                ) : h.objectType === "statement" ? (
                  <StatementLink id={h.publicId} text={h.searchText} />
                ) : h.subjectQid ? (
                  <EntityLink id={h.subjectQid} labels={entities[h.subjectQid]?.labels} lang={i18n.language} />
                ) : (
                  h.publicId
                )}
              </td>
              <td>{h.searchText}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
