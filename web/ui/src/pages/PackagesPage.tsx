import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Pkg = { code: string; lifecycle: string; labels?: Record<string, string> };

export function PackagesPage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<Pkg[]>([]);
  const [code, setCode] = useState("");
  const [label, setLabel] = useState("");
  const [error, setError] = useState("");

  async function load() {
    try {
      const res = await apiFetch<{ items: Pkg[] }>("/v1/packages");
      setItems(res.items || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function create(e: FormEvent) {
    e.preventDefault();
    try {
      await apiFetch("/v1/packages", {
        method: "POST",
        body: JSON.stringify({ code, lifecycle: "released", labels: { en: label || code } }),
      });
      setCode("");
      setLabel("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("packages.title")}</h1>
      <form className="panel row" onSubmit={create}>
        <label className="field">
          Code
          <input value={code} onChange={(e) => setCode(e.target.value)} required />
        </label>
        <label className="field">
          {t("entities.labelEn")}
          <input value={label} onChange={(e) => setLabel(e.target.value)} />
        </label>
        <button className="primary">{t("common.save")}</button>
      </form>
      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>Code</th>
            <th>Label</th>
            <th>Lifecycle</th>
          </tr>
        </thead>
        <tbody>
          {items.map((p) => (
            <tr key={p.code}>
              <td>{p.code}</td>
              <td>{p.labels?.en}</td>
              <td>{p.lifecycle}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
