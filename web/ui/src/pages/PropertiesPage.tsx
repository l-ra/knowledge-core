import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Property = {
  id: string;
  datatype: string;
  labels?: Record<string, string>;
  constraints?: Record<string, unknown>;
};

const DATATYPES = [
  "String",
  "EntityReference",
  "Boolean",
  "Integer",
  "Decimal",
  "Date",
  "DateTime",
  "URI",
  "LocalizedString",
  "ExternalIdentifier",
  "Quantity",
  "Interval",
];

export function PropertiesPage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<Property[]>([]);
  const [label, setLabel] = useState("");
  const [datatype, setDatatype] = useState("String");
  const [constraintsJSON, setConstraintsJSON] = useState("{}");
  const [error, setError] = useState("");

  async function load() {
    try {
      const res = await apiFetch<{ items: Property[] }>("/v1/properties?limit=200");
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
      let constraints = {};
      try {
        constraints = JSON.parse(constraintsJSON);
      } catch {
        throw new Error("invalid constraints JSON");
      }
      await apiFetch("/v1/properties", {
        method: "POST",
        body: JSON.stringify({ datatype, labels: { en: label }, constraints }),
      });
      setLabel("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("properties.title")}</h1>
      <form className="panel row" onSubmit={create}>
        <label className="field">
          {t("entities.labelEn")}
          <input value={label} onChange={(e) => setLabel(e.target.value)} required />
        </label>
        <label className="field">
          {t("properties.datatype")}
          <select value={datatype} onChange={(e) => setDatatype(e.target.value)}>
            {DATATYPES.map((d) => (
              <option key={d} value={d}>
                {d}
              </option>
            ))}
          </select>
        </label>
        <details className="advanced">
          <summary>{t("properties.constraints")}</summary>
          <label className="field" style={{ marginTop: "0.75rem" }}>
            JSON
            <textarea rows={4} value={constraintsJSON} onChange={(e) => setConstraintsJSON(e.target.value)} />
          </label>
        </details>
        <button className="primary">{t("properties.create")}</button>
      </form>
      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Label</th>
            <th>Datatype</th>
            <th>{t("properties.constraints")}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((p) => (
            <tr key={p.id}>
              <td>
                <Link to={`/entities/${p.id}`}>{p.id}</Link>
              </td>
              <td>{p.labels?.en}</td>
              <td>{p.datatype}</td>
              <td className="muted">{p.constraints ? JSON.stringify(p.constraints) : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="muted">
        <Link to="/entities">{t("nav.entities")}</Link>
      </p>
    </div>
  );
}
