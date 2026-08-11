import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

export function SchemaConfigPage() {
  const { t } = useTranslation();
  const [instanceOf, setInstanceOf] = useState("");
  const [updatedAt, setUpdatedAt] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  async function load() {
    try {
      const cfg = await apiFetch<{ instanceOfProperty: string; updatedAt?: string }>("/v1/admin/schema-config");
      setInstanceOf(cfg.instanceOfProperty || "");
      setUpdatedAt(cfg.updatedAt || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function save(e: FormEvent) {
    e.preventDefault();
    setSaved(false);
    try {
      const cfg = await apiFetch<{ instanceOfProperty: string; updatedAt?: string }>("/v1/admin/schema-config", {
        method: "PUT",
        body: JSON.stringify({ instanceOfProperty: instanceOf }),
      });
      setUpdatedAt(cfg.updatedAt || "");
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="stack">
      <h1>{t("schema.title")}</h1>
      <p className="muted">{t("schema.subtitle")}</p>
      <form className="panel stack" onSubmit={save}>
        <label className="field">
          {t("schema.instanceOfProperty")}
          <input value={instanceOf} onChange={(e) => setInstanceOf(e.target.value)} placeholder="P1" />
        </label>
        {updatedAt && <p className="muted">{t("schema.updated")}: {updatedAt}</p>}
        <button className="primary">{t("common.save")}</button>
      </form>
      {saved && <p className="pill">{t("schema.saved")}</p>}
      {error && <p className="error">{error}</p>}
    </div>
  );
}
