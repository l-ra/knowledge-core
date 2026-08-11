import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type ClassDef = {
  id: string;
  labels?: Record<string, string>;
  document?: { subClassOf?: string };
  canonicalEntityId?: string;
};

export function ClassesPage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<ClassDef[]>([]);
  const [label, setLabel] = useState("");
  const [subClassOf, setSubClassOf] = useState("");
  const [canonicalEntityId, setCanonicalEntityId] = useState("");
  const [error, setError] = useState("");

  async function load() {
    try {
      const res = await apiFetch<{ items: ClassDef[] }>("/v1/classes?limit=200");
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
      await apiFetch("/v1/classes", {
        method: "POST",
        body: JSON.stringify({
          labels: { en: label },
          subClassOf: subClassOf || undefined,
          canonicalEntityId: canonicalEntityId || undefined,
        }),
      });
      setLabel("");
      setSubClassOf("");
      setCanonicalEntityId("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("classes.title")}</h1>
      <form className="panel row" onSubmit={create}>
        <label className="field">
          {t("entities.labelEn")}
          <input value={label} onChange={(e) => setLabel(e.target.value)} required />
        </label>
        <label className="field">
          {t("classes.subClassOf")}
          <input value={subClassOf} onChange={(e) => setSubClassOf(e.target.value)} placeholder="C1" />
        </label>
        <label className="field">
          {t("classes.canonicalEntity")}
          <input value={canonicalEntityId} onChange={(e) => setCanonicalEntityId(e.target.value)} placeholder="Q1" />
        </label>
        <button className="primary">{t("classes.create")}</button>
      </form>
      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Label</th>
            <th>{t("classes.subClassOf")}</th>
            <th>{t("classes.canonicalEntity")}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((c) => (
            <tr key={c.id}>
              <td>{c.id}</td>
              <td>{c.labels?.en}</td>
              <td>{c.document?.subClassOf || "—"}</td>
              <td>{c.canonicalEntityId || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="muted">
        <Link to="/model/shapes">{t("nav.shapes")}</Link>
      </p>
    </div>
  );
}
