import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "./api";
import { ApiValue, displayValueText } from "./ValueEditor";
import { EntityLink, ReferenceLink, StatementLink } from "./links";
import { useEntityLookup } from "./useEntityLookup";

export type StatementItem = {
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

export type PropertyMeta = { id: string; datatype: string; labels?: Record<string, string> };

type Props = {
  query: string;
  properties?: PropertyMeta[];
  hidePropertyIds?: string[];
  emptyText: string;
  subjectBreadcrumb?: string[];
};

export function StatementList({ query, properties, hidePropertyIds, emptyText, subjectBreadcrumb }: Props) {
  const { t, i18n } = useTranslation();
  const [items, setItems] = useState<StatementItem[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const hidden = useMemo(() => new Set(hidePropertyIds || []), [hidePropertyIds]);

  async function load(reset = false, cursor = "") {
    setLoading(true);
    setError("");
    try {
      const sp = new URLSearchParams(query);
      sp.set("limit", "20");
      if (cursor) sp.set("cursor", cursor);
      const res = await apiFetch<{ items: StatementItem[]; nextCursor?: string }>(`/v1/statements?${sp.toString()}`);
      setItems((prev) => (reset ? res.items || [] : [...prev, ...(res.items || [])]));
      setNextCursor(res.nextCursor || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load(true);
  }, [query]);

  const visibleItems = useMemo(() => items.filter((item) => !hidden.has(item.property)), [items, hidden]);
  const relatedIds = useMemo(() => {
    const ids: string[] = [];
    for (const st of visibleItems) {
      ids.push(st.subject, st.property);
      if (st.value?.type === "EntityReference" && st.value.entityId) {
        ids.push(String(st.value.entityId));
      }
      for (const q of st.qualifiers || []) {
        ids.push(q.property);
        if (q.value?.type === "EntityReference" && q.value.entityId) {
          ids.push(String(q.value.entityId));
        }
      }
    }
    return ids;
  }, [visibleItems]);
  const entities = useEntityLookup(relatedIds);

  function renderValue(value: ApiValue) {
    if (value?.type === "EntityReference" && value.entityId) {
      const id = String(value.entityId);
      return <EntityLink id={id} labels={entities[id]?.labels} lang={i18n.language} breadcrumb={subjectBreadcrumb} />;
    }
    if (value?.type === "Quantity" && value.unitEntityId) {
      const unitId = String(value.unitEntityId);
      return (
        <span>
          {String(value.quantityValue ?? "")}{" "}
          <EntityLink id={unitId} labels={entities[unitId]?.labels} lang={i18n.language} breadcrumb={subjectBreadcrumb} />
        </span>
      );
    }
    return <span>{displayValueText(value, i18n.language)}</span>;
  }

  return (
    <div className="stack">
      {error && <p className="error">{error}</p>}
      {visibleItems.length === 0 && !loading && <p className="muted">{emptyText}</p>}
      {visibleItems.map((st) => (
        <div className="statement panel" key={st.id}>
          <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
            <div className="stack compact">
              <StatementLink id={st.id} />
              <div className="row compact">
                <span className="muted">{t("statement.subject")}:</span>
                <EntityLink
                  id={st.subject}
                  labels={entities[st.subject]?.labels}
                  lang={i18n.language}
                  breadcrumb={subjectBreadcrumb}
                />
              </div>
              <div className="row compact">
                <span className="muted">{t("entity.property")}:</span>
                <EntityLink
                  id={st.property}
                  labels={properties?.find((p) => p.id === st.property)?.labels || entities[st.property]?.labels}
                  lang={i18n.language}
                  breadcrumb={subjectBreadcrumb}
                />
              </div>
            </div>
            <span className="muted">rev {st.revisionNo}</span>
          </div>
          <div>
            <strong>{t("entity.value")}:</strong> {renderValue(st.value)}
          </div>
          {(st.qualifiers?.length || 0) > 0 && (
            <div className="stack compact">
              <strong>{t("statement.qualifiers")}</strong>
              {st.qualifiers!.map((q, index) => (
                <div key={`${q.property}:${index}`} className="row compact">
                  <EntityLink id={q.property} labels={entities[q.property]?.labels} lang={i18n.language} />:
                  {renderValue(q.value)}
                </div>
              ))}
            </div>
          )}
          {(st.referenceIds?.length || 0) > 0 && (
            <div className="stack compact">
              <strong>{t("statement.references")}</strong>
              <div className="row compact">
                {st.referenceIds!.map((id) => (
                  <ReferenceLink key={id} id={id} />
                ))}
              </div>
            </div>
          )}
          {(st.validFrom || st.validTo) && (
            <p className="muted">
              {t("statement.validity")}: {st.validFrom || "—"} - {st.validTo || "—"}
            </p>
          )}
        </div>
      ))}
      {nextCursor && (
        <button type="button" onClick={() => void load(false, nextCursor)} disabled={loading}>
          {t("common.next")}
        </button>
      )}
    </div>
  );
}
