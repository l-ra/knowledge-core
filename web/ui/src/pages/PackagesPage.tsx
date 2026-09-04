import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { usePackage } from "../package";

type Pkg = {
  code: string;
  lifecycle: string;
  labels?: Record<string, string>;
  iriBase?: string;
  latestReleaseVersion?: string;
  modifiedAfterRelease?: boolean;
};

type RDFCandidate = {
  iriBase: string;
  suggestedCode: string;
  iriCount: number;
  tripleCount: number;
  sampleIris?: string[];
  suggestedAction: "create" | "update" | "use" | string;
  existingPackageCode?: string;
  existingIriBase?: string;
  source?: string;
  turtlePrefix?: string;
};

type RDFAssignmentRow = {
  key: string;
  iriBase: string;
  packageCode: string;
  create: boolean;
  setIriBase: boolean;
  enabled: boolean;
  suggestedAction: string;
  tripleCount: number;
  source?: string;
  turtlePrefix?: string;
};

type RDFGlobalResult = {
  dryRun: boolean;
  packages?: Array<{
    iriBase: string;
    packageCode: string;
    action: string;
    error?: string;
    import?: {
      changeSetId?: string;
      wouldCreate?: unknown[];
      created?: unknown[];
      matched?: unknown[];
      skipped?: unknown[];
      warnings?: string[];
      errors?: string[];
    };
  }>;
  warnings?: string[];
  errors?: string[];
};

let rowSeq = 0;
function newRowKey() {
  rowSeq += 1;
  return `row-${rowSeq}`;
}

function candidateToRow(c: RDFCandidate): RDFAssignmentRow {
  return {
    key: newRowKey(),
    iriBase: c.iriBase,
    packageCode: c.existingPackageCode || c.suggestedCode,
    create: c.suggestedAction === "create",
    setIriBase: c.suggestedAction !== "use",
    enabled: true,
    suggestedAction: c.suggestedAction,
    tripleCount: c.tripleCount || 0,
    source: c.source,
    turtlePrefix: c.turtlePrefix,
  };
}

export function PackagesPage() {
  const { t } = useTranslation();
  const { reload: reloadPackages, setPackageCode, packageCode } = usePackage();
  const [items, setItems] = useState<Pkg[]>([]);
  const [code, setCode] = useState("");
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [iriBase, setIriBase] = useState("");
  const [error, setError] = useState("");
  const [importError, setImportError] = useState("");
  const [importOk, setImportOk] = useState("");

  const [rdfText, setRdfText] = useState("");
  const [turtlePrefixes, setTurtlePrefixes] = useState("");
  const [rdfRows, setRdfRows] = useState<RDFAssignmentRow[]>([]);
  const [rdfBusy, setRdfBusy] = useState(false);
  const [rdfError, setRdfError] = useState("");
  const [rdfPreview, setRdfPreview] = useState<RDFGlobalResult | null>(null);
  const [rdfInfo, setRdfInfo] = useState("");

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
          descriptions: description ? { en: description } : undefined,
          iriBase: iriBase || undefined,
        }),
      });
      setPackageCode(code);
      setCode("");
      setLabel("");
      setDescription("");
      setIriBase("");
      await load();
      await reloadPackages();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function removePackage(code: string) {
    setError("");
    if (!window.confirm(t("packages.deleteConfirm", { code }))) return;
    try {
      await apiFetch(`/v1/packages/${encodeURIComponent(code)}`, { method: "DELETE" });
      if (packageCode === code) {
        setPackageCode("");
      }
      await load();
      await reloadPackages();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function exportBundle(code: string, version: string) {
    setError("");
    try {
      const bundle = await apiFetch<unknown>(
        `/v1/packages/${encodeURIComponent(code)}/releases/${encodeURIComponent(version)}/bundle`,
      );
      const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${code}-${version}-bundle.json`;
      a.click();
      URL.revokeObjectURL(url);
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

  async function runAnalyze(ntriples: string, turtle: string, replaceRows: boolean) {
    setRdfError("");
    setRdfInfo("");
    setRdfPreview(null);
    if (!ntriples.trim() && !turtle.trim()) {
      throw new Error(t("packages.rdfNeedInput"));
    }
    setRdfBusy(true);
    try {
      const res = await apiFetch<{
        candidates?: RDFCandidate[];
        errors?: string[];
        warnings?: string[];
        packages?: Pkg[];
      }>("/v1/rdf/analyze", {
        method: "POST",
        body: JSON.stringify({
          ntriples: ntriples || undefined,
          turtlePrefixes: turtle || undefined,
        }),
      });
      if (res.errors?.length) {
        setRdfError(res.errors.join("; "));
        return;
      }
      const next = (res.candidates || []).map(candidateToRow);
      if (replaceRows) {
        setRdfRows(next);
      } else {
        setRdfRows((prev) => mergeRows(prev, next));
      }
      if (res.warnings?.length) {
        setRdfInfo(res.warnings.slice(0, 8).join(" · "));
      }
    } finally {
      setRdfBusy(false);
    }
  }

  async function onRDFFile(file: File | null) {
    if (!file) return;
    try {
      const ntriples = await file.text();
      setRdfText(ntriples);
      await runAnalyze(ntriples, turtlePrefixes, true);
    } catch (err) {
      setRdfError(err instanceof Error ? err.message : t("common.error"));
      setRdfBusy(false);
    }
  }

  async function onRDFAnalyze() {
    try {
      await runAnalyze(rdfText, turtlePrefixes, true);
    } catch (err) {
      setRdfError(err instanceof Error ? err.message : t("common.error"));
      setRdfBusy(false);
    }
  }

  async function onExtractTurtle() {
    setRdfError("");
    setRdfInfo("");
    if (!turtlePrefixes.trim()) {
      setRdfError(t("packages.rdfTurtleRequired"));
      return;
    }
    setRdfBusy(true);
    try {
      const res = await apiFetch<{
        prefixes?: Array<{ prefix?: string; iriBase: string; suggestedCode: string }>;
        errors?: string[];
        warnings?: string[];
      }>("/v1/rdf/prefixes", {
        method: "POST",
        body: JSON.stringify({ turtlePrefixes }),
      });
      if (res.errors?.length) {
        setRdfError(res.errors.join("; "));
        return;
      }
      const next = (res.prefixes || []).map(
        (p): RDFAssignmentRow => ({
          key: newRowKey(),
          iriBase: p.iriBase,
          packageCode: p.suggestedCode,
          create: true,
          setIriBase: true,
          enabled: true,
          suggestedAction: "create",
          tripleCount: 0,
          source: "turtle",
          turtlePrefix: p.prefix,
        }),
      );
      setRdfRows((prev) => mergeRows(prev, next));
      if (res.warnings?.length) {
        setRdfInfo(res.warnings.slice(0, 8).join(" · "));
      } else {
        setRdfInfo(t("packages.rdfTurtleExtracted", { count: next.length }));
      }
    } catch (err) {
      setRdfError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setRdfBusy(false);
    }
  }

  function updateRow(idx: number, patch: Partial<RDFAssignmentRow>) {
    setRdfRows((prev) => prev.map((r, i) => (i === idx ? { ...r, ...patch } : r)));
  }

  function removeRow(idx: number) {
    setRdfRows((prev) => prev.filter((_, i) => i !== idx));
  }

  function addEmptyRow() {
    setRdfRows((prev) => [
      ...prev,
      {
        key: newRowKey(),
        iriBase: "",
        packageCode: "",
        create: true,
        setIriBase: true,
        enabled: true,
        suggestedAction: "manual",
        tripleCount: 0,
        source: "manual",
      },
    ]);
  }

  async function runRDFGlobal(dryRun: boolean) {
    setRdfError("");
    setRdfInfo("");
    setRdfBusy(true);
    try {
      if (!rdfText.trim()) {
        throw new Error(t("packages.rdfNeedNTriples"));
      }
      const assignments = rdfRows
        .filter((r) => r.enabled)
        .map((r) => ({
          iriBase: r.iriBase.trim(),
          packageCode: r.packageCode.trim(),
          create: r.create,
          setIriBase: r.setIriBase,
        }));
      if (assignments.length === 0 || assignments.some((a) => !a.iriBase || !a.packageCode)) {
        throw new Error(t("packages.rdfNoAssignments"));
      }
      const res = await apiFetch<RDFGlobalResult>("/v1/rdf/import", {
        method: "POST",
        body: JSON.stringify({ ntriples: rdfText, dryRun, assignments }),
      });
      setRdfPreview(res);
      if (res.errors?.length) {
        setRdfError(res.errors.join("; "));
      }
      if (!dryRun && !res.errors?.length) {
        setRdfInfo(t("packages.rdfGlobalOk"));
        await load();
        await reloadPackages();
      }
    } catch (err) {
      setRdfError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setRdfBusy(false);
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
          {t("packages.descriptionEn")}
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={t("packages.descriptionPlaceholder")}
          />
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

      <details className="panel collapsible-section">
        <summary>
          <h2>{t("packages.rdfImport")}</h2>
        </summary>
        <div className="stack collapsible-body">
        <p className="muted">{t("packages.rdfGlobalHint")}</p>
        <label className="field">
          {t("packages.rdfFile")}
          <input
            type="file"
            accept=".nt,text/plain,application/n-triples"
            disabled={rdfBusy}
            onChange={(e) => void onRDFFile(e.target.files?.[0] || null)}
          />
        </label>
        <label className="field">
          {t("packages.rdfNTriples")}
          <textarea
            rows={6}
            className="mono"
            value={rdfText}
            disabled={rdfBusy}
            onChange={(e) => setRdfText(e.target.value)}
            placeholder="<https://example.org/id/a> <http://www.w3.org/2000/01/rdf-schema#label> &quot;A&quot; ."
          />
        </label>
        <label className="field">
          {t("packages.rdfTurtlePrefixes")}
          <textarea
            rows={4}
            className="mono"
            value={turtlePrefixes}
            disabled={rdfBusy}
            onChange={(e) => setTurtlePrefixes(e.target.value)}
            placeholder={"@prefix ex: <https://example.org/id/> .\n@prefix person: <https://example.org/id/person/> ."}
          />
        </label>
        <p className="muted">{t("packages.rdfTurtleHint")}</p>
        <div className="row">
          <button type="button" disabled={rdfBusy} onClick={() => void onRDFAnalyze()}>
            {t("packages.rdfAnalyze")}
          </button>
          <button type="button" disabled={rdfBusy} onClick={() => void onExtractTurtle()}>
            {t("packages.rdfExtractTurtle")}
          </button>
          <button type="button" disabled={rdfBusy} onClick={addEmptyRow}>
            {t("packages.rdfAddPrefix")}
          </button>
        </div>
        {rdfError && <p className="error">{rdfError}</p>}
        {rdfInfo && <p className="muted">{rdfInfo}</p>}

        {rdfRows.length > 0 && (
          <>
            <table className="table">
              <thead>
                <tr>
                  <th />
                  <th>{t("packages.iriBase")}</th>
                  <th>{t("packages.rdfAction")}</th>
                  <th>Code</th>
                  <th>{t("packages.rdfTriples")}</th>
                  <th>{t("packages.rdfSetIriBase")}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {rdfRows.map((r, idx) => (
                  <tr key={r.key}>
                    <td>
                      <input
                        type="checkbox"
                        checked={r.enabled}
                        onChange={(e) => updateRow(idx, { enabled: e.target.checked })}
                      />
                    </td>
                    <td>
                      <input
                        className="mono"
                        style={{ minWidth: "18rem" }}
                        value={r.iriBase}
                        onChange={(e) => updateRow(idx, { iriBase: e.target.value })}
                        placeholder="https://example.org/id/"
                      />
                      {(r.turtlePrefix || r.source) && (
                        <div className="muted" style={{ fontSize: "0.85em" }}>
                          {r.turtlePrefix ? `@prefix ${r.turtlePrefix}:` : r.source}
                        </div>
                      )}
                    </td>
                    <td>
                      <select
                        value={r.create ? "create" : "use"}
                        onChange={(e) =>
                          updateRow(idx, {
                            create: e.target.value === "create",
                            setIriBase: e.target.value === "create" ? true : r.setIriBase,
                          })
                        }
                      >
                        <option value="create">{t("packages.rdfCreatePkg")}</option>
                        <option value="use">{t("packages.rdfUsePkg")}</option>
                      </select>
                      <div className="muted" style={{ fontSize: "0.85em" }}>
                        {r.suggestedAction}
                      </div>
                    </td>
                    <td>
                      {r.create ? (
                        <input
                          value={r.packageCode}
                          onChange={(e) => updateRow(idx, { packageCode: e.target.value })}
                          required
                        />
                      ) : (
                        <select
                          value={r.packageCode}
                          onChange={(e) => updateRow(idx, { packageCode: e.target.value })}
                        >
                          {!items.some((p) => p.code === r.packageCode) && r.packageCode && (
                            <option value={r.packageCode}>{r.packageCode}</option>
                          )}
                          {items.map((p) => (
                            <option key={p.code} value={p.code}>
                              {p.code}
                              {p.iriBase ? ` (${p.iriBase})` : ""}
                            </option>
                          ))}
                        </select>
                      )}
                    </td>
                    <td>{r.tripleCount}</td>
                    <td>
                      <input
                        type="checkbox"
                        checked={r.setIriBase}
                        disabled={r.create}
                        onChange={(e) => updateRow(idx, { setIriBase: e.target.checked })}
                      />
                    </td>
                    <td>
                      <button type="button" disabled={rdfBusy} onClick={() => removeRow(idx)}>
                        {t("packages.rdfRemovePrefix")}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="row">
              <button type="button" disabled={rdfBusy} onClick={() => void runRDFGlobal(true)}>
                {t("packages.rdfDryRun")}
              </button>
              <button
                type="button"
                className="primary"
                disabled={rdfBusy || !rdfPreview?.dryRun}
                onClick={() => void runRDFGlobal(false)}
              >
                {t("packages.rdfImportCommit")}
              </button>
            </div>
          </>
        )}

        {rdfPreview && (
          <div className="stack">
            {(rdfPreview.packages || []).map((p) => (
              <div key={p.iriBase} className="panel stack">
                <strong>
                  {p.packageCode} · {p.action}
                </strong>
                <span className="mono muted">{p.iriBase}</span>
                {p.error && <p className="error">{p.error}</p>}
                {p.import && (
                  <p className="muted">
                    {t("packages.rdfImportSummary", {
                      matched: p.import.matched?.length ?? 0,
                      create: (p.import.wouldCreate || p.import.created)?.length ?? 0,
                      skipped: p.import.skipped?.length ?? 0,
                    })}
                    {p.import.changeSetId ? ` · ${p.import.changeSetId}` : ""}
                  </p>
                )}
              </div>
            ))}
          </div>
        )}
        </div>
      </details>

      <details className="panel collapsible-section">
        <summary>
          <h2>{t("packages.import")}</h2>
        </summary>
        <div className="stack collapsible-body">
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
        </div>
      </details>

      <table className="table">
        <thead>
          <tr>
            <th>Code</th>
            <th>Label</th>
            <th>{t("packages.iriBase")}</th>
            <th>Lifecycle</th>
            <th>{t("packages.latestRelease")}</th>
            <th />
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
              <td>
                {p.latestReleaseVersion ? (
                  <span className="pkg-release-cell">
                    <span className="mono">{p.latestReleaseVersion}</span>
                    {p.modifiedAfterRelease && (
                      <span
                        className="pkg-dirty-icon"
                        title={t("packages.modifiedAfterRelease")}
                        aria-label={t("packages.modifiedAfterRelease")}
                      >
                        <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
                          <path
                            fill="currentColor"
                            d="M11.5 1.5a1.5 1.5 0 0 1 2.12 2.12L5.62 11.62 3 13l1.38-2.62L11.5 1.5zm1.06.94-7.5 7.5-.44 1.44 1.44-.44 7.5-7.5a.5.5 0 0 0-.7-.7z"
                          />
                        </svg>
                      </span>
                    )}
                  </span>
                ) : (
                  <span className="muted">—</span>
                )}
              </td>
              <td>
                <div className="row">
                  <button
                    type="button"
                    disabled={!p.latestReleaseVersion}
                    title={
                      p.latestReleaseVersion
                        ? t("packages.exportBundle")
                        : t("packages.noReleases")
                    }
                    onClick={() => {
                      if (!p.latestReleaseVersion) return;
                      void exportBundle(p.code, p.latestReleaseVersion);
                    }}
                  >
                    {t("packages.exportBundle")}
                  </button>
                  <button type="button" className="danger" onClick={() => void removePackage(p.code)}>
                    {t("packages.delete")}
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function mergeRows(prev: RDFAssignmentRow[], next: RDFAssignmentRow[]): RDFAssignmentRow[] {
  const byBase = new Map<string, number>();
  const out = [...prev];
  for (let i = 0; i < out.length; i++) {
    const b = out[i].iriBase.trim();
    if (b) byBase.set(b, i);
  }
  for (const n of next) {
    const b = n.iriBase.trim();
    if (!b) {
      out.push(n);
      continue;
    }
    const idx = byBase.get(b);
    if (idx === undefined) {
      byBase.set(b, out.length);
      out.push(n);
      continue;
    }
    const cur = out[idx];
    out[idx] = {
      ...cur,
      tripleCount: Math.max(cur.tripleCount, n.tripleCount),
      turtlePrefix: n.turtlePrefix || cur.turtlePrefix,
      source: cur.source && n.source && cur.source !== n.source ? `${cur.source}+${n.source}` : n.source || cur.source,
      suggestedAction: cur.suggestedAction === "manual" ? n.suggestedAction : cur.suggestedAction,
    };
  }
  return out;
}
