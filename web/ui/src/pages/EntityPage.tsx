import { FormEvent, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Entity = {
  id: string;
  revisionNo: number;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
};

type Statement = {
  id: string;
  property: string;
  revisionNo: number;
  value: Record<string, unknown>;
  qualifiers?: Array<{ property: string; value: Record<string, unknown> }>;
  referenceIds?: string[];
  validFrom?: string;
  validTo?: string;
};

type Property = { id: string; datatype: string; labels?: Record<string, string> };

function valueInput(datatype: string, value: string, setValue: (v: string) => void) {
  if (datatype === "Boolean") {
    return (
      <select value={value} onChange={(e) => setValue(e.target.value)}>
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    );
  }
  return <input value={value} onChange={(e) => setValue(e.target.value)} required />;
}

function toApiValue(datatype: string, raw: string): Record<string, unknown> {
  switch (datatype) {
    case "EntityReference":
      return { type: "EntityReference", entityId: raw };
    case "Boolean":
      return { type: "Boolean", bool: raw === "true" };
    case "Integer":
      return { type: "Integer", int64: Number(raw) };
    case "URI":
      return { type: "URI", uri: raw };
    default:
      return { type: "String", string: raw };
  }
}

function displayValue(v: Record<string, unknown>): string {
  if (v.string != null) return String(v.string);
  if (v.entityId != null) return String(v.entityId);
  if (v.uri != null) return String(v.uri);
  if (v.bool != null) return String(v.bool);
  if (v.int64 != null) return String(v.int64);
  return JSON.stringify(v);
}

export function EntityPage() {
  const { qid = "" } = useParams();
  const { t } = useTranslation();
  const [entity, setEntity] = useState<Entity | null>(null);
  const [statements, setStatements] = useState<Statement[]>([]);
  const [properties, setProperties] = useState<Property[]>([]);
  const [labelEn, setLabelEn] = useState("");
  const [error, setError] = useState("");
  const [prop, setProp] = useState("");
  const [val, setVal] = useState("");
  const [qualJSON, setQualJSON] = useState("[]");
  const [refs, setRefs] = useState("");
  const [validFrom, setValidFrom] = useState("");
  const [validTo, setValidTo] = useState("");

  async function reload() {
    setError("");
    try {
      const [ent, sts, props] = await Promise.all([
        apiFetch<Entity>(`/v1/entities/${qid}`),
        apiFetch<{ statements: Statement[] }>(`/v1/entities/${qid}/statements`),
        apiFetch<{ items: Property[] }>("/v1/properties?limit=200"),
      ]);
      setEntity(ent);
      setLabelEn(ent.labels?.en || "");
      setStatements(sts.statements || []);
      setProperties(props.items || []);
      if (!prop && props.items?.[0]) setProp(props.items[0].id);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qid]);

  const selected = properties.find((p) => p.id === prop);

  async function saveLabels(e: FormEvent) {
    e.preventDefault();
    if (!entity) return;
    try {
      await apiFetch(`/v1/entities/${qid}`, {
        method: "PATCH",
        body: JSON.stringify({
          labels: { ...(entity.labels || {}), en: labelEn },
          expectedRevision: entity.revisionNo,
        }),
      });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function addStatement(e: FormEvent) {
    e.preventDefault();
    if (!selected) return;
    try {
      const body: Record<string, unknown> = {
        subject: qid,
        property: prop,
        value: toApiValue(selected.datatype, val),
      };
      try {
        const quals = JSON.parse(qualJSON);
        if (Array.isArray(quals) && quals.length) body.qualifiers = quals;
      } catch {
        throw new Error("invalid qualifiers JSON");
      }
      const refIds = refs
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (refIds.length) body.referenceIds = refIds;
      if (validFrom) body.validFrom = new Date(validFrom).toISOString();
      if (validTo) body.validTo = new Date(validTo).toISOString();
      await apiFetch("/v1/statements", { method: "POST", body: JSON.stringify(body) });
      setVal("");
      setQualJSON("[]");
      setRefs("");
      setValidFrom("");
      setValidTo("");
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function revise(st: Statement, newVal: string) {
    const p = properties.find((x) => x.id === st.property);
    if (!p) return;
    try {
      await apiFetch(`/v1/statements/${st.id}/revise`, {
        method: "POST",
        body: JSON.stringify({
          expectedRevision: st.revisionNo,
          value: toApiValue(p.datatype, newVal),
        }),
      });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  if (!entity && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <p className="muted">
        <Link to="/entities">{t("nav.entities")}</Link> / {qid}
      </p>
      <h1>{entity?.labels?.en || qid}</h1>
      {error && <p className="error">{error}</p>}

      {entity && (
        <form className="panel row" onSubmit={saveLabels}>
          <label className="field">
            {t("entities.labelEn")}
            <input value={labelEn} onChange={(e) => setLabelEn(e.target.value)} />
          </label>
          <button className="primary" type="submit">
            {t("entity.saveLabels")}
          </button>
          <span className="muted">rev {entity.revisionNo}</span>
        </form>
      )}

      <section className="panel stack">
        <h2>{t("entity.addStatement")}</h2>
        <form className="stack" onSubmit={addStatement}>
          <div className="row">
            <label className="field">
              {t("entity.property")}
              <select value={prop} onChange={(e) => setProp(e.target.value)}>
                {properties.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.id} — {p.labels?.en || p.datatype}
                  </option>
                ))}
              </select>
            </label>
            <label className="field">
              {t("entity.value")}
              {valueInput(selected?.datatype || "String", val, setVal)}
            </label>
          </div>
          <details className="advanced">
            <summary>{t("entity.advanced")}</summary>
            <div className="stack" style={{ marginTop: "0.75rem" }}>
              <label className="field">
                {t("entity.qualifiers")}
                <textarea rows={3} value={qualJSON} onChange={(e) => setQualJSON(e.target.value)} />
              </label>
              <label className="field">
                {t("entity.references")}
                <input value={refs} onChange={(e) => setRefs(e.target.value)} />
              </label>
              <div className="row">
                <label className="field">
                  {t("entity.validFrom")}
                  <input type="datetime-local" value={validFrom} onChange={(e) => setValidFrom(e.target.value)} />
                </label>
                <label className="field">
                  {t("entity.validTo")}
                  <input type="datetime-local" value={validTo} onChange={(e) => setValidTo(e.target.value)} />
                </label>
              </div>
            </div>
          </details>
          <button className="primary" type="submit">
            {t("common.save")}
          </button>
        </form>
      </section>

      <section className="stack">
        <h2>{t("entity.statements")}</h2>
        {statements.map((st) => (
          <StatementRow key={st.id} st={st} properties={properties} onRevise={revise} t={t} />
        ))}
      </section>

      <p>
        <Link to={`/entities/${qid}/history`}>{t("entity.history")}</Link>
      </p>
    </div>
  );
}

function StatementRow({
  st,
  properties,
  onRevise,
  t,
}: {
  st: Statement;
  properties: Property[];
  onRevise: (st: Statement, v: string) => Promise<void>;
  t: (k: string) => string;
}) {
  const [edit, setEdit] = useState(displayValue(st.value));
  const p = properties.find((x) => x.id === st.property);
  return (
    <div className="statement">
      <div className="row">
        <strong>
          {st.property} {p?.labels?.en ? `(${p.labels.en})` : ""}
        </strong>
        <span className="muted">{st.id} · rev {st.revisionNo}</span>
      </div>
      <div className="row">
        <input value={edit} onChange={(e) => setEdit(e.target.value)} style={{ flex: 1 }} />
        <button type="button" onClick={() => void onRevise(st, edit)}>
          {t("entity.revise")}
        </button>
      </div>
      <details className="advanced">
        <summary>{t("entity.advanced")}</summary>
        <pre className="muted" style={{ whiteSpace: "pre-wrap" }}>
          {JSON.stringify(
            {
              qualifiers: st.qualifiers || [],
              referenceIds: st.referenceIds || [],
              validFrom: st.validFrom,
              validTo: st.validTo,
              value: st.value,
            },
            null,
            2,
          )}
        </pre>
      </details>
    </div>
  );
}
