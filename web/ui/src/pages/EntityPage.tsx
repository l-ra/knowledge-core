import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { pickLabel } from "../labels";
import { usePackage } from "../package";
import { useChangeSetDraft } from "../changeset";
import { ApiValue, ValueEditor, displayValueText, emptyValue, valueFromApi } from "../ValueEditor";

type Finding = { code: string; severity: string; message: string };

type Entity = {
  id: string;
  kind?: string;
  revisionNo: number;
  packageCode?: string;
  iriLocal?: string;
  iri?: string;
  iriAliases?: Array<{ iri: string; kind: string }>;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
  status?: string;
  propertyProfile?: { datatype: string; constraints?: Record<string, unknown> };
  classProfile?: { subClassOf?: string };
};

type Statement = {
  id: string;
  property: string;
  revisionNo: number;
  value: ApiValue;
  qualifiers?: Array<{ property: string; value: ApiValue }>;
  referenceIds?: string[];
  validFrom?: string;
  validTo?: string;
};

type Property = { id: string; datatype: string; labels?: Record<string, string> };

type ObjectRelease = { packageCode: string; version: string; revisionNo: number };

type LocState = { breadcrumb?: string[] };

export function EntityPage() {
  const { qid = "" } = useParams();
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const nav = useNavigate();
  const { packageCode } = usePackage();
  const { isOpen, runWrite } = useChangeSetDraft();
  const breadcrumb = (location.state as LocState | null)?.breadcrumb || [];

  const [entity, setEntity] = useState<Entity | null>(null);
  const [statements, setStatements] = useState<Statement[]>([]);
  const [properties, setProperties] = useState<Property[]>([]);
  const [modelProps, setModelProps] = useState<string[]>([]);
  const [releases, setReleases] = useState<ObjectRelease[]>([]);
  const [hideModel, setHideModel] = useState(true);
  const [error, setError] = useState("");
  const [info, setInfo] = useState("");
  const [mode, setMode] = useState<"view" | "editLabels" | "addStatement" | string>("view");
  const [labelEn, setLabelEn] = useState("");
  const [iriLocal, setIriLocal] = useState("");
  const [aliasDraft, setAliasDraft] = useState("");
  const [aliases, setAliases] = useState<Array<{ iri: string; kind: string }>>([]);
  const [prop, setProp] = useState("");
  const [val, setVal] = useState<ApiValue>(emptyValue("String"));
  const [qualJSON, setQualJSON] = useState("[]");
  const [refs, setRefs] = useState("");
  const [validFrom, setValidFrom] = useState("");
  const [validTo, setValidTo] = useState("");
  const [validationMode, setValidationMode] = useState<"relaxed" | "strict" | "off">("relaxed");
  const [validationMsg, setValidationMsg] = useState<Finding[]>([]);
  const [reviseVal, setReviseVal] = useState<ApiValue | null>(null);

  async function reload() {
    setError("");
    try {
      const [ent, sts, props, cfg, rels] = await Promise.all([
        apiFetch<Entity>(`/v1/entities/${qid}`),
        apiFetch<{ statements: Statement[] }>(`/v1/entities/${qid}/statements`),
        apiFetch<{ items: Property[] }>("/v1/properties?limit=200"),
        apiFetch<{ modelProperties?: string[]; instanceOfProperty?: string }>("/v1/admin/schema-config").catch(() => ({
          modelProperties: [] as string[],
        })),
        apiFetch<{ items: ObjectRelease[] }>(`/v1/objects/${qid}/releases`).catch(() => ({ items: [] as ObjectRelease[] })),
      ]);
      setEntity(ent);
      setLabelEn(ent.labels?.en || "");
      setIriLocal(ent.iriLocal || "");
      setAliases(ent.iriAliases || []);
      setStatements(sts.statements || []);
      setProperties(props.items || []);
      setModelProps(cfg.modelProperties || []);
      setReleases(rels.items || []);
      if (!prop && props.items?.[0]) {
        setProp(props.items[0].id);
        setVal(emptyValue(props.items[0].datatype));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    setMode("view");
    setInfo("");
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qid]);

  const selected = properties.find((p) => p.id === prop);
  const propLabel = (pid: string) => {
    const p = properties.find((x) => x.id === pid);
    return pickLabel(p?.labels, i18n.language, pid);
  };

  const visibleStatements = useMemo(() => {
    let list = statements;
    if (hideModel && modelProps.length) {
      const hide = new Set(modelProps);
      list = list.filter((st) => !hide.has(st.property));
    }
    return [...list].sort((a, b) => propLabel(a.property).localeCompare(propLabel(b.property), i18n.language));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statements, hideModel, modelProps, properties, i18n.language]);

  const packageMismatch =
    !!entity?.packageCode && !!packageCode && entity.packageCode !== packageCode;

  function goToEntity(id: string) {
    nav(`/entities/${id}`, { state: { breadcrumb: [...breadcrumb, qid] } });
  }

  async function saveLabels(e: FormEvent) {
    e.preventDefault();
    if (!entity) return;
    setInfo("");
    const labels = { ...(entity.labels || {}), en: labelEn };
    const body: Record<string, unknown> = {
      labels,
      iriLocal,
      expectedRevision: entity.revisionNo,
    };
    try {
      await runWrite(
        async () => {
          await apiFetch(`/v1/entities/${qid}`, {
            method: "PATCH",
            body: JSON.stringify(body),
          });
          setMode("view");
          await reload();
        },
        {
          op: "updateEntity",
          entity: qid,
          labels,
          iriLocal: iriLocal || undefined,
          expectedRevision: entity.revisionNo,
        },
      );
      if (isOpen) {
        setInfo(t("changeset.queued"));
        setMode("view");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function saveAliases(e: FormEvent) {
    e.preventDefault();
    setInfo("");
    setError("");
    try {
      await apiFetch(`/v1/entities/${qid}/iri-aliases`, {
        method: "PUT",
        body: JSON.stringify({ aliases }),
      });
      setInfo(t("entity.aliasesSaved"));
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function addStatement(e: FormEvent) {
    e.preventDefault();
    if (!selected) return;
    if (!packageCode) {
      setError(t("package.required"));
      return;
    }
    setInfo("");
    try {
      let quals: unknown[] = [];
      try {
        const parsed = JSON.parse(qualJSON);
        if (Array.isArray(parsed) && parsed.length) quals = parsed;
      } catch {
        throw new Error("invalid qualifiers JSON");
      }
      const refIds = refs
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      const validFromIso = validFrom ? new Date(validFrom).toISOString() : undefined;
      const validToIso = validTo ? new Date(validTo).toISOString() : undefined;

      await runWrite(
        async () => {
          const body: Record<string, unknown> = {
            packageCode,
            subject: qid,
            property: prop,
            value: val,
          };
          if (quals.length) body.qualifiers = quals;
          if (refIds.length) body.referenceIds = refIds;
          if (validFromIso) body.validFrom = validFromIso;
          if (validToIso) body.validTo = validToIso;
          const res = await apiFetch<{ validation?: { findings: Finding[] } }>("/v1/statements", {
            method: "POST",
            headers: { "X-Validation-Mode": validationMode },
            body: JSON.stringify(body),
          });
          setValidationMsg(res.validation?.findings || []);
          setVal(emptyValue(selected.datatype));
          setQualJSON("[]");
          setRefs("");
          setValidFrom("");
          setValidTo("");
          setMode("view");
          await reload();
        },
        {
          op: "createStatement",
          packageCode,
          subject: qid,
          property: prop,
          value: val,
          qualifiers: quals.length ? quals : undefined,
          referenceIds: refIds.length ? refIds : undefined,
          validFrom: validFromIso,
          validTo: validToIso,
        },
      );
      if (isOpen) {
        setInfo(t("changeset.queued"));
        setVal(emptyValue(selected.datatype));
        setQualJSON("[]");
        setRefs("");
        setValidFrom("");
        setValidTo("");
        setMode("view");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function revise(st: Statement) {
    const p = properties.find((x) => x.id === st.property);
    if (!p || !reviseVal) return;
    setInfo("");
    try {
      await runWrite(
        async () => {
          await apiFetch(`/v1/statements/${st.id}/revise`, {
            method: "POST",
            body: JSON.stringify({
              expectedRevision: st.revisionNo,
              value: reviseVal,
            }),
          });
          setMode("view");
          setReviseVal(null);
          await reload();
        },
        {
          op: "reviseStatement",
          statement: st.id,
          expectedRevision: st.revisionNo,
          value: reviseVal,
        },
      );
      if (isOpen) {
        setInfo(t("changeset.queued"));
        setMode("view");
        setReviseVal(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  function renderValue(v: ApiValue) {
    if (v?.type === "EntityReference" && v.entityId) {
      const id = String(v.entityId);
      return (
        <button type="button" className="linkish" onClick={() => goToEntity(id)}>
          {displayValueText(v, i18n.language)}
        </button>
      );
    }
    return <span>{displayValueText(v, i18n.language)}</span>;
  }

  if (!entity && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <nav className="breadcrumb muted" aria-label="breadcrumb">
        <Link to="/entities" state={{ breadcrumb: [] }}>
          {t("nav.entities")}
        </Link>
        {breadcrumb.map((id) => (
          <span key={id}>
            {" / "}
            <Link to={`/entities/${id}`} state={{ breadcrumb: breadcrumb.slice(0, breadcrumb.indexOf(id)) }}>
              {id}
            </Link>
          </span>
        ))}
        <span>
          {" / "}
          <strong>{qid}</strong>
        </span>
      </nav>

      <header className="panel entity-header">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <h1>{pickLabel(entity?.labels, i18n.language, qid)}</h1>
            <p className="muted">
              {qid}
              {entity?.kind ? ` · ${entity.kind}` : ""}
              {entity?.status ? ` · ${entity.status}` : ""}
              {entity?.packageCode ? ` · ${entity.packageCode}` : ""}
              {entity ? ` · rev ${entity.revisionNo}` : ""}
            </p>
            {entity?.iri && (
              <p className="muted mono" title={t("entity.iriHint")}>
                {t("entity.iri")}: {entity.iri}
              </p>
            )}
            {entity?.descriptions?.en && <p>{entity.descriptions.en}</p>}
            {releases.length > 0 && (
              <div className="row release-badges">
                {releases.map((r) => (
                  <Link
                    key={`${r.packageCode}@${r.version}`}
                    className="badge"
                    to={`/model/packages/${encodeURIComponent(r.packageCode)}`}
                  >
                    {t("entity.inRelease", { pkg: r.packageCode, version: r.version })}
                  </Link>
                ))}
              </div>
            )}
          </div>
          <div className="row">
            <button type="button" disabled={!packageCode} onClick={() => setMode("editLabels")}>
              {t("entity.editLabels")}
            </button>
            <button
              type="button"
              className="primary"
              disabled={!packageCode}
              onClick={() => {
                setMode("addStatement");
                if (selected) setVal(emptyValue(selected.datatype));
              }}
            >
              {t("entity.addStatement")}
            </button>
          </div>
        </div>

        {entity?.propertyProfile && (
          <div className="profile-block">
            <h3>{t("entity.propertyProfile")}</h3>
            <p>
              <strong>{t("properties.datatype")}:</strong> {entity.propertyProfile.datatype}
            </p>
            {entity.propertyProfile.constraints && Object.keys(entity.propertyProfile.constraints).length > 0 && (
              <pre className="muted">{JSON.stringify(entity.propertyProfile.constraints, null, 2)}</pre>
            )}
          </div>
        )}
        {entity?.classProfile && (
          <div className="profile-block">
            <h3>{t("entity.classProfile")}</h3>
            <p>
              <strong>{t("classes.subClassOf")}:</strong>{" "}
              {entity.classProfile.subClassOf ? (
                <button type="button" className="linkish" onClick={() => goToEntity(entity.classProfile!.subClassOf!)}>
                  {entity.classProfile.subClassOf}
                </button>
              ) : (
                "—"
              )}
            </p>
          </div>
        )}
      </header>

      {info && <p className="muted">{info}</p>}
      {error && <p className="error">{error}</p>}
      {validationMsg.length > 0 && (
        <div className="panel stack validation-panel">
          <h3>{t("validation.afterSave")}</h3>
          <ul>
            {validationMsg.map((f, i) => (
              <li key={i} className={f.severity === "error" ? "validation-error" : "validation-warning"}>
                [{f.severity}] {f.message}
              </li>
            ))}
          </ul>
          <Link to={`/entities/${qid}/validation`}>{t("validation.fullReport")}</Link>
        </div>
      )}

      {mode === "editLabels" && entity && (
        <form className="panel stack" onSubmit={saveLabels}>
          <label className="field">
            {t("entities.labelEn")}
            <input value={labelEn} onChange={(e) => setLabelEn(e.target.value)} required />
          </label>
          <label className="field">
            {t("entity.iriLocal")}
            <input value={iriLocal} onChange={(e) => setIriLocal(e.target.value)} placeholder={qid} />
            <span className="muted">{t("entity.iriLocalHint")}</span>
          </label>
          <div className="row">
            <button className="primary" type="submit" disabled={!packageCode}>
              {t("entity.saveLabels")}
            </button>
            <button type="button" onClick={() => setMode("view")}>
              {t("common.cancel")}
            </button>
          </div>
        </form>
      )}

      {entity && (
        <section className="panel stack">
          <h3>{t("entity.iriAliases")}</h3>
          <p className="muted">{t("entity.iriAliasesHint")}</p>
          <ul>
            {aliases.map((a) => (
              <li key={a.iri} className="row" style={{ justifyContent: "space-between" }}>
                <span className="mono">
                  {a.kind}: {a.iri}
                </span>
                <button type="button" onClick={() => setAliases((prev) => prev.filter((x) => x.iri !== a.iri))}>
                  {t("common.remove")}
                </button>
              </li>
            ))}
          </ul>
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              const iri = aliasDraft.trim();
              if (!iri) return;
              setAliases((prev) => [...prev.filter((x) => x.iri !== iri), { iri, kind: "sameAs" }]);
              setAliasDraft("");
            }}
          >
            <label className="field" style={{ flex: 1 }}>
              {t("entity.addAlias")}
              <input
                value={aliasDraft}
                onChange={(e) => setAliasDraft(e.target.value)}
                placeholder="https://www.wikidata.org/entity/Q42"
              />
            </label>
            <button type="submit">{t("common.add")}</button>
          </form>
          <button type="button" className="primary" disabled={!packageCode} onClick={(e) => void saveAliases(e)}>
            {t("entity.saveAliases")}
          </button>
        </section>
      )}

      {mode === "addStatement" && (
        <section className="panel stack">
          <h2>{t("entity.addStatement")}</h2>
          {packageMismatch && (
            <p className="error">
              {t("entity.packageMismatch", { entityPkg: entity?.packageCode, activePkg: packageCode })}
            </p>
          )}
          <form className="stack" onSubmit={addStatement}>
            <div className="row">
              <label className="field">
                {t("entity.property")}
                <select
                  value={prop}
                  onChange={(e) => {
                    const id = e.target.value;
                    setProp(id);
                    const p = properties.find((x) => x.id === id);
                    setVal(emptyValue(p?.datatype || "String"));
                  }}
                >
                  {properties.map((p) => (
                    <option key={p.id} value={p.id}>
                      {pickLabel(p.labels, i18n.language, p.id)} ({p.id})
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                {t("entity.value")}
                <ValueEditor
                  datatype={selected?.datatype || "String"}
                  value={val}
                  onChange={setVal}
                />
              </label>
            </div>
            <details className="advanced">
              <summary>{t("entity.advanced")}</summary>
              <div className="stack" style={{ marginTop: "0.75rem" }}>
                <label className="field">
                  {t("validation.mode")}
                  <select value={validationMode} onChange={(e) => setValidationMode(e.target.value as typeof validationMode)}>
                    <option value="relaxed">{t("validation.relaxed")}</option>
                    <option value="strict">{t("validation.strict")}</option>
                    <option value="off">{t("validation.off")}</option>
                  </select>
                </label>
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
            <div className="row">
              <button className="primary" type="submit" disabled={!packageCode}>
                {t("common.save")}
              </button>
              <button type="button" onClick={() => setMode("view")}>
                {t("common.cancel")}
              </button>
            </div>
          </form>
        </section>
      )}

      <section className="stack">
        <div className="row" style={{ justifyContent: "space-between" }}>
          <h2>{t("entity.statements")}</h2>
          <label className="row" style={{ gap: "0.4rem" }}>
            <input type="checkbox" checked={hideModel} onChange={(e) => setHideModel(e.target.checked)} />
            {t("entity.hideModel")}
          </label>
        </div>
        {visibleStatements.map((st) => {
          const editing = mode === `revise:${st.id}`;
          const p = properties.find((x) => x.id === st.property);
          return (
            <div className="statement" key={st.id}>
              <div className="row" style={{ justifyContent: "space-between" }}>
                <strong>
                  <button type="button" className="linkish" onClick={() => goToEntity(st.property)}>
                    {propLabel(st.property)}
                  </button>
                  <span className="muted"> ({st.property})</span>
                </strong>
                <span className="muted">
                  {st.id} · rev {st.revisionNo}
                </span>
              </div>
              {!editing && (
                <div className="row" style={{ justifyContent: "space-between" }}>
                  <div>{renderValue(st.value)}</div>
                  <button
                    type="button"
                    disabled={!packageCode}
                    onClick={() => {
                      setMode(`revise:${st.id}`);
                      setReviseVal(valueFromApi(p?.datatype || "String", st.value));
                    }}
                  >
                    {t("entity.revise")}
                  </button>
                </div>
              )}
              {editing && reviseVal && (
                <form
                  className="stack"
                  onSubmit={(e) => {
                    e.preventDefault();
                    void revise(st);
                  }}
                >
                  <ValueEditor datatype={p?.datatype || "String"} value={reviseVal} onChange={setReviseVal} />
                  <div className="row">
                    <button className="primary" type="submit">
                      {t("common.save")}
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setMode("view");
                        setReviseVal(null);
                      }}
                    >
                      {t("common.cancel")}
                    </button>
                  </div>
                </form>
              )}
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
        })}
      </section>

      <p>
        <Link to={`/entities/${qid}/history`}>{t("entity.history")}</Link>
        {" · "}
        <Link to={`/entities/${qid}/validation`}>{t("validation.title")}</Link>
      </p>
    </div>
  );
}
