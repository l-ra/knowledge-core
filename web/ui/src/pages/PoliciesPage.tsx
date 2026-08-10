import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Policy = { name: string; priority: number; document: unknown };

export function PoliciesPage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<Policy[]>([]);
  const [name, setName] = useState("");
  const [doc, setDoc] = useState(
    '{\n  "effect": "allow",\n  "operations": ["discover", "read"],\n  "roles": ["viewer"]\n}',
  );
  const [error, setError] = useState("");

  async function load() {
    try {
      const res = await apiFetch<{ policies: Policy[] }>("/v1/policies");
      setItems(res.policies || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function save(e: FormEvent) {
    e.preventDefault();
    try {
      await apiFetch("/v1/policies", {
        method: "POST",
        body: JSON.stringify({ name, priority: 100, document: JSON.parse(doc) }),
      });
      setName("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function remove(n: string) {
    try {
      await apiFetch(`/v1/policies/${encodeURIComponent(n)}`, { method: "DELETE" });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("policies.title")}</h1>
      <form className="panel stack" onSubmit={save}>
        <label className="field">
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <label className="field">
          Document (JSON)
          <textarea rows={8} value={doc} onChange={(e) => setDoc(e.target.value)} />
        </label>
        <button className="primary">{t("common.save")}</button>
      </form>
      {error && <p className="error">{error}</p>}
      {items.map((p) => (
        <div className="panel" key={p.name}>
          <div className="row">
            <strong>{p.name}</strong>
            <span className="muted">priority {p.priority}</span>
            <button type="button" className="danger" onClick={() => void remove(p.name)}>
              Delete
            </button>
          </div>
          <pre>{JSON.stringify(p.document, null, 2)}</pre>
        </div>
      ))}
    </div>
  );
}
