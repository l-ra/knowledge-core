import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { EntityLink } from "../links";
import { useEntityLookup } from "../useEntityLookup";

type Finding = {
  code: string;
  severity: string;
  message: string;
  propertyId?: string;
  shapeCode?: string;
};

type ValidationResult = {
  entityId: string;
  findings: Finding[];
  summary: { errors: number; warnings: number; infos: number };
};

export function ValidationPage() {
  const { qid: rawQid = "" } = useParams();
  const qid = (() => {
    try {
      return decodeURIComponent(rawQid);
    } catch {
      return rawQid;
    }
  })();
  const { t, i18n } = useTranslation();
  const [result, setResult] = useState<ValidationResult | null>(null);
  const [reportId, setReportId] = useState("");
  const [error, setError] = useState("");
  const entities = useEntityLookup([qid]);

  async function load() {
    setError("");
    try {
      const res = await apiFetch<ValidationResult>(`/v1/entities/${encodeURIComponent(qid)}/validation`);
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qid]);

  async function persistReport() {
    try {
      const rep = await apiFetch<{ id: string }>("/v1/validation/reports", {
        method: "POST",
        body: JSON.stringify({ entityIds: [qid], persist: true, scope: "entity" }),
      });
      setReportId(rep.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  if (!result && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <nav className="breadcrumb muted">
        <Link to="/entities">{t("nav.entities")}</Link>
        {" / "}
        <EntityLink id={qid} displayId={entities[qid]?.displayId} labels={entities[qid]?.labels} lang={i18n.language} />
        {" / "}
        {t("validation.title")}
      </nav>
      <h1>{t("validation.title")}</h1>
      {error && <p className="error">{error}</p>}
      {result && (
        <>
          <div className="panel row">
            <span className="pill">{t("validation.errors")}: {result.summary.errors}</span>
            <span className="pill">{t("validation.warnings")}: {result.summary.warnings}</span>
            <span className="pill">{t("validation.infos")}: {result.summary.infos}</span>
            <button type="button" onClick={() => void persistReport()}>
              {t("validation.saveReport")}
            </button>
          </div>
          {reportId && <p className="muted">{t("validation.reportId")}: {reportId}</p>}
          {result.findings.length === 0 ? (
            <p>{t("validation.clean")}</p>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>{t("validation.severity")}</th>
                  <th>{t("validation.code")}</th>
                  <th>{t("validation.message")}</th>
                </tr>
              </thead>
              <tbody>
                {result.findings.map((f, i) => (
                  <tr key={i} className={f.severity === "error" ? "validation-error" : f.severity === "warning" ? "validation-warning" : ""}>
                    <td>{f.severity}</td>
                    <td>{f.code}</td>
                    <td>{f.message}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </div>
  );
}
