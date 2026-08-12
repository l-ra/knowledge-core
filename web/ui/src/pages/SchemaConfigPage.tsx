import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

export function SchemaConfigPage() {
  const { t } = useTranslation();
  const [instanceOf, setInstanceOf] = useState("");
  const [modelProps, setModelProps] = useState("");
  const [updatedAt, setUpdatedAt] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  async function load() {
    try {
      const cfg = await apiFetch<{
        instanceOfProperty: string;
        modelProperties?: string[];
        updatedAt?: string;
      }>("/v1/admin/schema-config");
      setInstanceOf(cfg.instanceOfProperty || "");
      const extras = (cfg.modelProperties || []).filter((p) => p && p !== cfg.instanceOfProperty);
      setModelProps(extras.join(", "));
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
      const modelProperties = modelProps
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      const cfg = await apiFetch<{
        instanceOfProperty: string;
        modelProperties?: string[];
        updatedAt?: string;
      }>("/v1/admin/schema-config", {
        method: "PUT",
        body: JSON.stringify({ instanceOfProperty: instanceOf, modelProperties }),
      });
      setUpdatedAt(cfg.updatedAt || "");
      const extras = (cfg.modelProperties || []).filter((p) => p && p !== cfg.instanceOfProperty);
      setModelProps(extras.join(", "));
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
        <label className="field">
          {t("schema.modelProperties")}
          <input
            value={modelProps}
            onChange={(e) => setModelProps(e.target.value)}
            placeholder="P2, P3"
          />
          <span className="muted">{t("schema.modelPropertiesHint")}</span>
        </label>
        {updatedAt && <p className="muted">{t("schema.updated")}: {updatedAt}</p>}
        <button className="primary">{t("common.save")}</button>
      </form>
      {saved && <p className="pill">{t("schema.saved")}</p>}
      {error && <p className="error">{error}</p>}
    </div>
  );
}
