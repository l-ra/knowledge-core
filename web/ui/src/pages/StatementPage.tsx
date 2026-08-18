import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { ApiValue, displayValueText } from "../ValueEditor";
import { EntityLink, ReferenceLink } from "../links";
import { useEntityLookup } from "../useEntityLookup";

type Statement = {
  id: string;
  subject: string;
  property: string;
  revisionNo: number;
  value: ApiValue;
  qualifiers?: Array<{ property: string; value: ApiValue }>;
  referenceIds?: string[];
  validFrom?: string;
  validTo?: string;
};

export function StatementPage() {
  const { sid = "" } = useParams();
  const { t, i18n } = useTranslation();
  const [statement, setStatement] = useState<Statement | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setError("");
    void apiFetch<Statement>(`/v1/statements/${sid}`)
      .then(setStatement)
      .catch((err) => setError(err instanceof Error ? err.message : t("common.error")));
  }, [sid, t]);

  const relatedIds = useMemo(() => {
    if (!statement) return [];
    const ids = [statement.subject, statement.property];
    if (statement.value?.type === "EntityReference" && statement.value.entityId) {
      ids.push(String(statement.value.entityId));
    }
    for (const q of statement.qualifiers || []) {
      ids.push(q.property);
      if (q.value?.type === "EntityReference" && q.value.entityId) {
        ids.push(String(q.value.entityId));
      }
    }
    return ids;
  }, [statement]);
  const entities = useEntityLookup(relatedIds);

  function renderValue(value: ApiValue) {
    if (value?.type === "EntityReference" && value.entityId) {
      const id = String(value.entityId);
      return <EntityLink id={id} labels={entities[id]?.labels} lang={i18n.language} />;
    }
    return <span>{displayValueText(value, i18n.language)}</span>;
  }

  if (!statement && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <nav className="breadcrumb muted">
        <Link to="/entities">{t("nav.entities")}</Link>
        {statement && (
          <>
            {" / "}
            <EntityLink id={statement.subject} labels={entities[statement.subject]?.labels} lang={i18n.language} />
          </>
        )}
        <span>
          {" / "}
          <strong>{sid}</strong>
        </span>
      </nav>
      {error && <p className="error">{error}</p>}
      {statement && (
        <section className="panel stack">
          <h1>{t("statement.title")}</h1>
          <p className="muted">
            {statement.id} · rev {statement.revisionNo}
          </p>
          <div className="stack">
            <div>
              <strong>{t("statement.subject")}:</strong>{" "}
              <EntityLink id={statement.subject} labels={entities[statement.subject]?.labels} lang={i18n.language} />
            </div>
            <div>
              <strong>{t("entity.property")}:</strong>{" "}
              <EntityLink id={statement.property} labels={entities[statement.property]?.labels} lang={i18n.language} />
            </div>
            <div>
              <strong>{t("entity.value")}:</strong> {renderValue(statement.value)}
            </div>
            {(statement.validFrom || statement.validTo) && (
              <div>
                <strong>{t("statement.validity")}:</strong> {statement.validFrom || "—"} - {statement.validTo || "—"}
              </div>
            )}
          </div>
          {(statement.qualifiers?.length || 0) > 0 && (
            <div className="stack">
              <h3>{t("statement.qualifiers")}</h3>
              {statement.qualifiers!.map((q, index) => (
                <div className="statement-fragment" key={`${q.property}:${index}`}>
                  <EntityLink id={q.property} labels={entities[q.property]?.labels} lang={i18n.language} />: {renderValue(q.value)}
                </div>
              ))}
            </div>
          )}
          {(statement.referenceIds?.length || 0) > 0 && (
            <div className="stack">
              <h3>{t("statement.references")}</h3>
              <div className="stack compact">
                {statement.referenceIds!.map((id) => (
                  <ReferenceLink key={id} id={id} />
                ))}
              </div>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
