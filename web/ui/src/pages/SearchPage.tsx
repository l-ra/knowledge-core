import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Hit = {
  objectType: string;
  publicId: string;
  subjectQid?: string;
  propertyPid?: string;
  searchText: string;
};

export function SearchPage() {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<Hit[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

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
                {h.objectType === "entity" ? (
                  <Link to={`/entities/${h.publicId}`}>{h.publicId}</Link>
                ) : h.subjectQid ? (
                  <Link to={`/entities/${h.subjectQid}`}>{h.publicId}</Link>
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
