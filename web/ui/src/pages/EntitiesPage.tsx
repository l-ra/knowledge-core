import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Entity = { id: string; labels?: Record<string, string>; status?: string };

export function EntitiesPage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<Entity[]>([]);
  const [q, setQ] = useState("");
  const [next, setNext] = useState("");
  const [label, setLabel] = useState("");
  const [error, setError] = useState("");

  async function load(opts?: { reset?: boolean; cursor?: string }) {
    setError("");
    const params = new URLSearchParams();
    if (q) params.set("q", q);
    if (opts?.cursor) params.set("cursor", opts.cursor);
    params.set("limit", "30");
    try {
      const res = await apiFetch<{ items: Entity[]; nextCursor?: string }>(`/v1/entities?${params}`);
      setItems((prev) => (opts?.reset ? res.items : [...prev, ...res.items]));
      setNext(res.nextCursor || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load({ reset: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function create(e: FormEvent) {
    e.preventDefault();
    try {
      const res = await apiFetch<{ data: Entity }>("/v1/entities", {
        method: "POST",
        body: JSON.stringify({ labels: { en: label } }),
      });
      setLabel("");
      window.location.href = `/ui/entities/${res.data.id}`;
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("entities.title")}</h1>
      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault();
          void load({ reset: true });
        }}
      >
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("search.placeholder")} />
        <button className="primary">{t("search.submit")}</button>
      </form>

      <form className="panel row" onSubmit={create}>
        <label className="field">
          {t("entities.labelEn")}
          <input value={label} onChange={(e) => setLabel(e.target.value)} required />
        </label>
        <button className="primary" type="submit">
          {t("entities.create")}
        </button>
      </form>

      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Label</th>
          </tr>
        </thead>
        <tbody>
          {items.map((it) => (
            <tr key={it.id}>
              <td>
                <Link to={`/entities/${it.id}`}>{it.id}</Link>
              </td>
              <td>{it.labels?.en || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {next && (
        <button type="button" onClick={() => void load({ cursor: next })}>
          {t("common.next")}
        </button>
      )}
    </div>
  );
}
