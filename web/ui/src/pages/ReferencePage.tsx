import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Reference = {
  id: string;
  fields: Record<string, unknown>;
  createdAt?: string;
};

export function ReferencePage() {
  const { rid = "" } = useParams();
  const { t } = useTranslation();
  const [reference, setReference] = useState<Reference | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setError("");
    void apiFetch<Reference>(`/v1/references/${rid}`)
      .then(setReference)
      .catch((err) => setError(err instanceof Error ? err.message : t("common.error")));
  }, [rid, t]);

  if (!reference && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <nav className="breadcrumb muted">
        <Link to="/entities">{t("nav.entities")}</Link>
        <span>
          {" / "}
          <strong>{rid}</strong>
        </span>
      </nav>
      {error && <p className="error">{error}</p>}
      {reference && (
        <section className="panel stack">
          <h1>{t("reference.title")}</h1>
          <p className="muted">{reference.id}</p>
          {reference.createdAt && (
            <p className="muted">
              {t("reference.createdAt")}: {reference.createdAt}
            </p>
          )}
          <pre className="muted" style={{ whiteSpace: "pre-wrap" }}>
            {JSON.stringify(reference.fields || {}, null, 2)}
          </pre>
        </section>
      )}
    </div>
  );
}
