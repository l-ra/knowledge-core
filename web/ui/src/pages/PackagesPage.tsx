import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { usePackage } from "../package";

type Pkg = { code: string; lifecycle: string; labels?: Record<string, string>; iriBase?: string };

export function PackagesPage() {
  const { t } = useTranslation();
  const { reload: reloadPackages, setPackageCode } = usePackage();
  const [items, setItems] = useState<Pkg[]>([]);
  const [code, setCode] = useState("");
  const [label, setLabel] = useState("");
  const [iriBase, setIriBase] = useState("");
  const [error, setError] = useState("");
  const [importError, setImportError] = useState("");
  const [importOk, setImportOk] = useState("");

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
        body: JSON.stringify({
          code,
          lifecycle: "released",
          labels: { en: label || code },
          iriBase: iriBase || undefined,
        }),
      });
      setPackageCode(code);
      setCode("");
      setLabel("");
      setIriBase("");
      await load();
      await reloadPackages();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function onImportFile(file: File | null) {
    setImportError("");
    setImportOk("");
    if (!file) return;
    try {
      const text = await file.text();
      const bundle = JSON.parse(text);
      await apiFetch("/v1/releases/import", {
        method: "POST",
        body: JSON.stringify(bundle),
      });
      setImportOk(t("packages.importOk"));
      await load();
      await reloadPackages();
    } catch (err) {
      setImportError(err instanceof Error ? err.message : t("common.error"));
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
        <label className="field">
          {t("packages.iriBase")}
          <input
            value={iriBase}
            onChange={(e) => setIriBase(e.target.value)}
            placeholder="https://example.org/id/"
          />
        </label>
        <button className="primary">{t("common.save")}</button>
      </form>
      {error && <p className="error">{error}</p>}

      <section className="panel stack">
        <h2>{t("packages.import")}</h2>
        <p className="muted">{t("packages.importHint")}</p>
        <label className="field">
          {t("packages.bundleFile")}
          <input
            type="file"
            accept="application/json,.json"
            onChange={(e) => void onImportFile(e.target.files?.[0] || null)}
          />
        </label>
        {importError && <p className="error">{importError}</p>}
        {importOk && <p className="muted">{importOk}</p>}
      </section>

      <table className="table">
        <thead>
          <tr>
            <th>Code</th>
            <th>Label</th>
            <th>{t("packages.iriBase")}</th>
            <th>Lifecycle</th>
          </tr>
        </thead>
        <tbody>
          {items.map((p) => (
            <tr key={p.code}>
              <td>
                <Link to={`/model/packages/${encodeURIComponent(p.code)}`}>{p.code}</Link>
              </td>
              <td>{p.labels?.en}</td>
              <td className="mono muted">{p.iriBase || "—"}</td>
              <td>{p.lifecycle}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
