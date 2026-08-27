import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { pickLabel } from "../labels";
import { usePackage } from "../package";
import { useChangeSetDraft } from "../changeset";
import { ApiValue, ValueEditor, displayValueText, emptyValue, valueFromApi } from "../ValueEditor";
import { EntityLink, ObjectLabel, ReferenceLink } from "../links";
import { useEntityLookup } from "../useEntityLookup";
import { StatementList, type PropertyMeta, type StatementItem } from "../StatementList";

type Finding = { code: string; severity: string; message: string };

type Entity = {
  id: string;
  displayId?: string;
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
  effectiveClasses?: string[];
};

type Statement = StatementItem;
type Property = PropertyMeta;
type ClassItem = {
  id: string;
  displayId?: string;
  labels?: Record<string, string>;
  packageCode?: string;
  document?: { subClassOf?: string };
};
type EntityListItem = { id: string; displayId?: string; labels?: Record<string, string>; kind?: string; packageCode?: string };

type ObjectRelease = { packageCode: string; version: string; revisionNo: number };

type LocState = { breadcrumb?: string[] };

export function EntityPage() {
  const hideModelStorageKey = "kc.hideModelStatements";
  const { qid: rawQid = "" } = useParams();
  const qid = (() => {
    try {
      return decodeURIComponent(rawQid);
    } catch {
      return rawQid;
    }
  })();
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const nav = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { packageCode } = usePackage();
  const { isOpen, runWrite } = useChangeSetDraft();
  const breadcrumb = (location.state as LocState | null)?.breadcrumb || [];
  const tab = searchParams.get("tab") || "statements";

  const [entity, setEntity] = useState<Entity | null>(null);
  const [statements, setStatements] = useState<Statement[]>([]);
  const [statementNext, setStatementNext] = useState("");
  const [properties, setProperties] = useState<Property[]>([]);
  const [modelProps, setModelProps] = useState<string[]>([]);
  const [releases, setReleases] = useState<ObjectRelease[]>([]);
  const [hideModel, setHideModel] = useState(() => {
    try {
      const saved = localStorage.getItem(hideModelStorageKey);
      return saved === null ? true : saved !== "false";
    } catch {
      return true;
    }
  });
  const [error, setError] = useState("");
  const [info, setInfo] = useState("");
  const [mode, setMode] = useState<"view" | "editLabels" | "addStatement" | string>("view");
  const [labelsDraft, setLabelsDraft] = useState<Record<string, string>>({ en: "" });
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
  const [classes, setClasses] = useState<ClassItem[]>([]);

  const instanceSubclass = searchParams.get("instanceSubclass") || "";
  const instanceQuery = searchParams.get("instanceQ") || "";
  const instancePackage = searchParams.get("instancePackage") || "";
  const [instanceItems, setInstanceItems] = useState<EntityListItem[]>([]);
  const [instanceNext, setInstanceNext] = useState("");
  const [instanceError, setInstanceError] = useState("");

  async function reload() {
    setError("");
    try {
      const [ent, props, cfg, rels, cls] = await Promise.all([
        apiFetch<Entity>(`/v1/entities/${encodeURIComponent(qid)}`),
        apiFetch<{ items: Property[] }>("/v1/properties?limit=500"),
        apiFetch<{ modelProperties?: string[]; instanceOfProperty?: string }>("/v1/admin/schema-config").catch(() => ({
          modelProperties: [] as string[],
        })),
        apiFetch<{ items: ObjectRelease[] }>(`/v1/objects/${encodeURIComponent(qid)}/releases`).catch(() => ({ items: [] as ObjectRelease[] })),
        apiFetch<{ items: ClassItem[] }>("/v1/classes?limit=1000").catch(() => ({ items: [] as ClassItem[] })),
      ]);
      setEntity(ent);
      setLabelsDraft(Object.keys(ent.labels || {}).length ? { ...(ent.labels || {}) } : { en: "" });
      setIriLocal(ent.iriLocal || "");
      setAliases(ent.iriAliases || []);
      setProperties(props.items || []);
      setModelProps(cfg.modelProperties || []);
      setReleases(rels.items || []);
      setClasses(cls.items || []);
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

  async function loadStatements(reset = false, cursor = "") {
    try {
      const sp = new URLSearchParams();
      sp.set("subject", qid);
      sp.set("limit", "20");
      if (cursor) sp.set("cursor", cursor);
      const res = await apiFetch<{ items: Statement[]; nextCursor?: string }>(`/v1/statements?${sp.toString()}`);
      setStatements((prev) => (reset ? res.items || [] : [...prev, ...(res.items || [])]));
      setStatementNext(res.nextCursor || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void loadStatements(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qid]);

  useEffect(() => {
    try {
      localStorage.setItem(hideModelStorageKey, String(hideModel));
    } catch {
      // Ignore storage failures and keep the in-memory preference.
    }
  }, [hideModel, hideModelStorageKey]);

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

  const statementGroups = useMemo(() => {
    const groups = new Map<string, Statement[]>();
    for (const st of visibleStatements) {
      groups.set(st.property, [...(groups.get(st.property) || []), st]);
    }
    return Array.from(groups.entries()).map(([propertyId, items]) => ({
      propertyId,
      propertyMeta: properties.find((p) => p.id === propertyId),
      items,
    }));
  }, [visibleStatements, properties]);

  const statementRelatedIds = useMemo(() => {
    const ids: string[] = [...breadcrumb];
    for (const st of visibleStatements) {
      ids.push(st.subject, st.property);
      if (st.value?.type === "EntityReference" && st.value.entityId) ids.push(String(st.value.entityId));
      if (st.value?.type === "Quantity" && st.value.unitEntityId) ids.push(String(st.value.unitEntityId));
      for (const q of st.qualifiers || []) {
        ids.push(q.property);
        if (q.value?.type === "EntityReference" && q.value.entityId) ids.push(String(q.value.entityId));
      }
    }
    if (entity?.classProfile?.subClassOf) ids.push(entity.classProfile.subClassOf);
    for (const id of entity?.effectiveClasses || []) ids.push(id);
    for (const id of entity?.propertyProfile?.constraints?.domainClasses || []) ids.push(id);
    for (const id of entity?.propertyProfile?.constraints?.rangeClasses || []) ids.push(id);
    return ids;
  }, [entity, visibleStatements, breadcrumb]);
  const relatedEntities = useEntityLookup(statementRelatedIds);

  const packageMismatch =
    !!entity?.packageCode && !!packageCode && entity.packageCode !== packageCode;
  const canMutate = !!packageCode && entity?.status !== "deleted";

  async function saveLabels(e: FormEvent) {
    e.preventDefault();
    if (!entity) return;
    setInfo("");
    const labels = Object.fromEntries(
      Object.entries(labelsDraft)
        .map(([lang, text]) => [lang.trim().toLowerCase(), text.trim()])
        .filter(([lang, text]) => lang && text),
    );
    if (!labels.en) {
      setError(t("entity.labelEnRequired"));
      return;
    }
    const body: Record<string, unknown> = {
      labels,
      iriLocal,
      expectedRevision: entity.revisionNo,
    };
    try {
      await runWrite(
        async () => {
          await apiFetch(`/v1/entities/${encodeURIComponent(qid)}`, {
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
      await apiFetch(`/v1/entities/${encodeURIComponent(qid)}/iri-aliases`, {
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
          await loadStatements(true);
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
          await apiFetch(`/v1/statements/${encodeURIComponent(st.id)}/revise`, {
            method: "POST",
            body: JSON.stringify({
              expectedRevision: st.revisionNo,
              value: reviseVal,
            }),
          });
          setMode("view");
          setReviseVal(null);
          await reload();
          await loadStatements(true);
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

  async function deprecateStatement(st: Statement) {
    if (!window.confirm(t("entity.deprecateStatementConfirm"))) return;
    setInfo("");
    try {
      await runWrite(
        async () => {
          await apiFetch(`/v1/statements/${encodeURIComponent(st.id)}/deprecate`, {
            method: "POST",
            body: JSON.stringify({ expectedRevision: st.revisionNo }),
          });
          await reload();
          await loadStatements(true);
        },
        {
          op: "deprecateStatement",
          statement: st.id,
          expectedRevision: st.revisionNo,
        },
      );
      if (isOpen) {
        setInfo(t("changeset.queued"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function deprecateEntity() {
    if (!entity) return;
    if (!window.confirm(t("entity.deprecateConfirm"))) return;
    setInfo("");
    try {
      await runWrite(
        async () => {
          await apiFetch(`/v1/entities/${encodeURIComponent(qid)}/deprecate`, {
            method: "POST",
            body: JSON.stringify({ expectedRevision: entity.revisionNo }),
          });
          await reload();
        },
        {
          op: "deprecateEntity",
          entity: qid,
          expectedRevision: entity.revisionNo,
        },
      );
      if (isOpen) {
        setInfo(t("changeset.queued"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function deleteEntity() {
    if (!entity) return;
    if (!window.confirm(t("entity.deleteConfirm"))) return;
    setInfo("");
    try {
      await runWrite(
        async () => {
          await apiFetch(`/v1/entities/${encodeURIComponent(qid)}/delete`, {
            method: "POST",
            body: JSON.stringify({ expectedRevision: entity.revisionNo }),
          });
          nav("/entities");
        },
        {
          op: "deleteEntity",
          entity: qid,
          expectedRevision: entity.revisionNo,
        },
      );
      if (isOpen) {
        setInfo(t("changeset.queued"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  function renderValue(v: ApiValue) {
    if (v?.type === "EntityReference" && v.entityId) {
      const id = String(v.entityId);
      return (
        <EntityLink id={id} labels={relatedEntities[id]?.labels} lang={i18n.language} breadcrumb={[...breadcrumb, qid]} />
      );
    }
    if (v?.type === "Quantity" && v.unitEntityId) {
      const id = String(v.unitEntityId);
      return (
        <span>
          {String(v.quantityValue ?? "")}{" "}
          <EntityLink id={id} labels={relatedEntities[id]?.labels} lang={i18n.language} breadcrumb={[...breadcrumb, qid]} />
        </span>
      );
    }
    return <span>{displayValueText(v, i18n.language)}</span>;
  }

  function setTab(nextTab: string) {
    const next = new URLSearchParams(searchParams);
    next.set("tab", nextTab);
    setSearchParams(next);
  }

  function updateInstanceFilters(update: Record<string, string>) {
    const next = new URLSearchParams(searchParams);
    for (const [key, value] of Object.entries(update)) {
      if (value) next.set(key, value);
      else next.delete(key);
    }
    next.set("tab", "instances");
    setSearchParams(next);
  }

  async function loadInstances(reset = false, cursor = "") {
    if (!entity) return;
    setInstanceError("");
    try {
      const sp = new URLSearchParams();
      sp.set("limit", "20");
      sp.set("instanceOf", instanceSubclass || qid);
      sp.set("includeSubclasses", instanceSubclass ? "false" : "true");
      if (instanceQuery) sp.set("q", instanceQuery);
      if (instancePackage) sp.set("package", instancePackage);
      if (cursor) sp.set("cursor", cursor);
      const res = await apiFetch<{ items: EntityListItem[]; nextCursor?: string }>(`/v1/entities?${sp.toString()}`);
      setInstanceItems((prev) => (reset ? res.items || [] : [...prev, ...(res.items || [])]));
      setInstanceNext(res.nextCursor || "");
    } catch (err) {
      setInstanceError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    if (tab === "instances" && entity?.classProfile) void loadInstances(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, qid, instanceSubclass, instanceQuery, instancePackage, entity?.classProfile]);

  const childClasses = useMemo(
    () => classes.filter((item) => item.document?.subClassOf === qid),
    [classes, qid],
  );
  const descendantClasses = useMemo(() => {
    const children = new Map<string, ClassItem[]>();
    for (const item of classes) {
      const parent = item.document?.subClassOf;
      if (!parent) continue;
      children.set(parent, [...(children.get(parent) || []), item]);
    }
    const out: ClassItem[] = [];
    const walk = (id: string) => {
      for (const item of children.get(id) || []) {
        out.push(item);
        walk(item.id);
      }
    };
    walk(qid);
    return out;
  }, [classes, qid]);
  const classLineage = useMemo(() => {
    const byId = new Map(classes.map((item) => [item.id, item]));
    const chain: ClassItem[] = [];
    let current = entity?.classProfile?.subClassOf || "";
    while (current && byId.has(current)) {
      const item = byId.get(current)!;
      chain.unshift(item);
      current = item.document?.subClassOf || "";
    }
    return chain;
  }, [classes, entity?.classProfile?.subClassOf]);

  const tabItems = useMemo(() => {
    const tabs: Array<{ key: string; label: string }> = [
      { key: "statements", label: t("entity.statements") },
      { key: "references", label: t("entity.referencesTab") },
    ];
    if (entity?.classProfile) {
      tabs.push({ key: "hierarchy", label: t("entity.hierarchy") });
      tabs.push({ key: "instances", label: t("entity.instances") });
    }
    if (entity?.propertyProfile) {
      tabs.push({ key: "uses", label: t("entity.uses") });
    }
    tabs.push({ key: "detail", label: t("entity.detailTab") });
    return tabs;
  }, [entity, t]);

  if (!entity && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <nav className="breadcrumb muted" aria-label="breadcrumb">
        <Link to="/entities">
          {t("nav.entities")}
        </Link>
        {breadcrumb.map((id) => (
          <span key={id}>
            {" / "}
            <Link to={`/entities/${encodeURIComponent(id)}`} state={{ breadcrumb: breadcrumb.slice(0, breadcrumb.indexOf(id)) }}>
              {pickLabel(relatedEntities[id]?.labels, i18n.language, relatedEntities[id]?.displayId || id)}
            </Link>
          </span>
        ))}
        <span>
          {" / "}
          <strong>
            <ObjectLabel id={qid} displayId={entity?.displayId} labels={entity?.labels} lang={i18n.language} />
          </strong>
        </span>
      </nav>

      <header className="entity-header">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <h1>{pickLabel(entity?.labels, i18n.language, qid)}</h1>
            <p className="muted">
              {entity?.displayId || qid}
              {entity?.kind ? ` · ${entity.kind}` : ""}
              {entity?.status ? ` · ${entity.status}` : ""}
              {entity?.packageCode ? ` · ${entity.packageCode}` : ""}
              {entity ? ` · rev ${entity.revisionNo}` : ""}
            </p>
            {(entity?.effectiveClasses?.length || 0) > 0 && (
              <div className="row compact entity-class-list">
                <span className="muted">{t("classes.title")}:</span>
                {(entity.effectiveClasses || []).map((classId) => (
                  <EntityLink
                    key={classId}
                    id={classId}
                    displayId={relatedEntities[classId]?.displayId}
                    labels={relatedEntities[classId]?.labels}
                    lang={i18n.language}
                    breadcrumb={[...breadcrumb, qid]}
                  />
                ))}
              </div>
            )}
          </div>
          <div className="row">
            <button type="button" disabled={!canMutate} onClick={() => { setTab("detail"); setMode("editLabels"); }}>
              {t("entity.editLabels")}
            </button>
            <button
              type="button"
              className="primary"
              disabled={!canMutate}
              onClick={() => {
                setTab("statements");
                setMode("addStatement");
                if (selected) setVal(emptyValue(selected.datatype));
              }}
            >
              {t("entity.addStatement")}
            </button>
            {entity?.status === "active" && (
              <button type="button" disabled={!canMutate} onClick={() => void deprecateEntity()}>
                {t("entity.deprecate")}
              </button>
            )}
            {(entity?.status === "active" || entity?.status === "deprecated") && (
              <button type="button" className="danger" disabled={!canMutate} onClick={() => void deleteEntity()}>
                {t("entity.delete")}
              </button>
            )}
          </div>
        </div>
      </header>

      <div className="tab-bar">
        {tabItems.map(({ key, label }) => (
          <button
            key={key}
            type="button"
            className={tab === key ? "tab-active" : "tab-inactive"}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
      </div>

      {info && <p className="muted">{info}</p>}
      {error && <p className="error">{error}</p>}
      {entity?.status === "deprecated" && <p className="muted">{t("entity.deprecatedBanner")}</p>}
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
          <Link to={`/entities/${encodeURIComponent(qid)}/validation`}>{t("validation.fullReport")}</Link>
        </div>
      )}

      <div className="tab-content">

        {/* ── Detail tab ── */}
        {tab === "detail" && entity && (
          <section className="stack">
            {entity.iri && (
              <p className="muted mono" title={t("entity.iriHint")}>
                {t("entity.iri")}: {entity.iri}
              </p>
            )}
            {pickLabel(entity.descriptions, i18n.language, "").trim() && (
              <p>{pickLabel(entity.descriptions, i18n.language, "")}</p>
            )}
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

            {entity.propertyProfile && (
              <div className="panel stack">
                <h3>{t("entity.propertyProfile")}</h3>
                <div className="stack compact">
                  <div>
                    <strong>{t("properties.datatype")}:</strong> {entity.propertyProfile.datatype}
                  </div>
                  {(entity.propertyProfile.constraints.domainClasses || []).length > 0 && (
                    <div>
                      <strong>{t("properties.domainClasses")}:</strong>{" "}
                      <span className="row compact">
                        {entity.propertyProfile.constraints.domainClasses!.map((classId) => (
                          <EntityLink
                            key={classId}
                            id={classId}
                            labels={relatedEntities[classId]?.labels}
                            lang={i18n.language}
                          />
                        ))}
                      </span>
                    </div>
                  )}
                  {(entity.propertyProfile.constraints.rangeClasses || []).length > 0 && (
                    <div>
                      <strong>{t("properties.rangeClasses")}:</strong>{" "}
                      <span className="row compact">
                        {entity.propertyProfile.constraints.rangeClasses!.map((classId) => (
                          <EntityLink
                            key={classId}
                            id={classId}
                            labels={relatedEntities[classId]?.labels}
                            lang={i18n.language}
                          />
                        ))}
                      </span>
                    </div>
                  )}
                  {entity.propertyProfile.constraints.minCount != null && (
                    <div>
                      <strong>{t("properties.minCount")}:</strong> {entity.propertyProfile.constraints.minCount}
                    </div>
                  )}
                  {entity.propertyProfile.constraints.maxCount != null && (
                    <div>
                      <strong>{t("properties.maxCount")}:</strong> {entity.propertyProfile.constraints.maxCount}
                    </div>
                  )}
                  {entity.propertyProfile.constraints.severity && (
                    <div>
                      <strong>{t("validation.severity")}:</strong> {entity.propertyProfile.constraints.severity}
                    </div>
                  )}
                </div>
                {Object.keys(entity.propertyProfile.constraints || {}).length > 0 && (
                  <details className="advanced">
                    <summary>{t("properties.constraintsRaw")}</summary>
                    <pre className="muted">{JSON.stringify(entity.propertyProfile.constraints, null, 2)}</pre>
                  </details>
                )}
              </div>
            )}
            {entity.classProfile && (
              <div className="panel stack">
                <h3>{t("entity.classProfile")}</h3>
                <p>
                  <strong>{t("classes.subClassOf")}:</strong>{" "}
                  {entity.classProfile.subClassOf ? (
                    <EntityLink
                      id={entity.classProfile.subClassOf}
                      labels={relatedEntities[entity.classProfile.subClassOf]?.labels}
                      lang={i18n.language}
                    />
                  ) : (
                    "—"
                  )}
                </p>
              </div>
            )}

            {mode === "editLabels" && (
              <form className="panel stack" onSubmit={saveLabels}>
                <h3>{t("entity.labels")}</h3>
                {Object.entries(labelsDraft).map(([lang, text]) => (
                  <div className="row" key={lang}>
                    <label className="field" style={{ maxWidth: "8rem", flex: "0 0 8rem" }}>
                      {t("common.language")}
                      <input
                        value={lang}
                        onChange={(e) => {
                          const nextLang = e.target.value.toLowerCase();
                          setLabelsDraft((prev) => {
                            const next = { ...prev };
                            delete next[lang];
                            next[nextLang || "en"] = text;
                            return next;
                          });
                        }}
                      />
                    </label>
                    <label className="field">
                      {t("entity.label")}
                      <input
                        value={text}
                        onChange={(e) => setLabelsDraft((prev) => ({ ...prev, [lang]: e.target.value }))}
                        required={lang === "en"}
                      />
                    </label>
                    {lang !== "en" && (
                      <button type="button" onClick={() => setLabelsDraft((prev) => Object.fromEntries(Object.entries(prev).filter(([key]) => key !== lang)))}>
                        {t("common.remove")}
                      </button>
                    )}
                  </div>
                ))}
                <button type="button" onClick={() => setLabelsDraft((prev) => ({ ...prev, [`l${Object.keys(prev).length}`]: "" }))}>
                  {t("entity.addLang")}
                </button>
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

            <div className="panel stack">
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
            </div>

            <p>
              <Link to={`/entities/${encodeURIComponent(qid)}/history`}>{t("entity.history")}</Link>
              {" · "}
              <Link to={`/entities/${encodeURIComponent(qid)}/validation`}>{t("validation.title")}</Link>
            </p>
          </section>
        )}

        {/* ── Statements tab ── */}
        {tab === "statements" && (
          <section className="stack">
            {mode === "addStatement" && (
              <div className="panel stack">
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
              </div>
            )}

            <div className="row" style={{ justifyContent: "flex-end" }}>
              <label className="row" style={{ gap: "0.4rem" }}>
                <input type="checkbox" checked={hideModel} onChange={(e) => setHideModel(e.target.checked)} />
                {t("entity.hideModel")}
              </label>
            </div>
            {statementGroups.length === 0 && <p className="muted">{t("search.empty")}</p>}
            {statementGroups.map(({ propertyId, propertyMeta, items }) => (
              <div className="statement-group panel" key={propertyId}>
                <div className="statement-group-header">
                  <strong>
                    <EntityLink
                      id={propertyId}
                      labels={propertyMeta?.labels}
                      lang={i18n.language}
                      breadcrumb={[...breadcrumb, qid]}
                    />
                  </strong>
                </div>
                <div className="statement-group-items">
                  {items.map((st) => {
                    const editing = mode === `revise:${st.id}`;
                    const p = properties.find((x) => x.id === st.property);
                    return (
                      <div className="statement-row" key={st.id}>
                        {!editing ? (
                          <>
                            <div className="statement-value-line">
                              <span className="statement-value">{renderValue(st.value)}</span>
                              <span className="statement-meta muted">
                                <Link to={`/statements/${encodeURIComponent(st.id)}`}>{st.id}</Link> · rev {st.revisionNo}
                              </span>
                              <div className="statement-actions">
                                <button
                                  type="button"
                                  className="statement-revise-button"
                                  disabled={!canMutate}
                                  onClick={() => {
                                    setMode(`revise:${st.id}`);
                                    setReviseVal(valueFromApi(p?.datatype || "String", st.value));
                                  }}
                                >
                                  {t("entity.revise")}
                                </button>
                                <button
                                  type="button"
                                  className="statement-revise-button danger"
                                  disabled={!canMutate}
                                  onClick={() => void deprecateStatement(st)}
                                >
                                  {t("entity.deprecateStatement")}
                                </button>
                              </div>
                            </div>
                            {((st.qualifiers?.length || 0) > 0 || (st.referenceIds?.length || 0) > 0 || st.validFrom || st.validTo) && (
                              <details className="advanced">
                                <summary>{t("entity.advanced")}</summary>
                                <div className="stack compact">
                                  {(st.qualifiers?.length || 0) > 0 && (
                                    <div className="stack compact">
                                      <strong>{t("statement.qualifiers")}</strong>
                                      {st.qualifiers!.map((q, index) => (
                                        <div key={`${q.property}:${index}`} className="row compact">
                                          <EntityLink id={q.property} labels={relatedEntities[q.property]?.labels} lang={i18n.language} />:
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
                              </details>
                            )}
                          </>
                        ) : (
                          <form
                            className="stack compact"
                            onSubmit={(e) => {
                              e.preventDefault();
                              void revise(st);
                            }}
                          >
                            <div className="statement-meta muted">
                              <Link to={`/statements/${encodeURIComponent(st.id)}`}>{st.id}</Link> · rev {st.revisionNo}
                            </div>
                            <ValueEditor datatype={p?.datatype || "String"} value={reviseVal} onChange={setReviseVal} />
                            <div className="row compact">
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
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
            {statementNext && (
              <button type="button" onClick={() => void loadStatements(false, statementNext)}>
                {t("common.next")}
              </button>
            )}
          </section>
        )}

        {/* ── References tab ── */}
        {tab === "references" && (
          <section className="stack">
            <StatementList
              query={new URLSearchParams({ object: qid }).toString()}
              properties={properties}
              emptyText={t("entity.noReferences")}
              subjectBreadcrumb={[...breadcrumb, qid]}
            />
          </section>
        )}

        {/* ── Uses tab (property only) ── */}
        {tab === "uses" && entity?.propertyProfile && (
          <section className="stack">
            <StatementList query={new URLSearchParams({ property: qid }).toString()} emptyText={t("entity.noUses")} />
          </section>
        )}

        {/* ── Hierarchy tab (class only) ── */}
        {tab === "hierarchy" && entity?.classProfile && (
          <section className="stack">
            <div className="panel stack">
              <div>
                <strong>{t("entity.ancestors")}:</strong>{" "}
                {classLineage.length > 0 ? (
                  <span className="row compact">
                    {classLineage.map((item) => (
                      <EntityLink key={item.id} id={item.id} labels={item.labels} lang={i18n.language} />
                    ))}
                  </span>
                ) : (
                  "—"
                )}
              </div>
              <div>
                <strong>{t("classes.subClassOf")}:</strong>{" "}
                {entity.classProfile.subClassOf ? (
                  <EntityLink
                    id={entity.classProfile.subClassOf}
                    labels={relatedEntities[entity.classProfile.subClassOf]?.labels}
                    lang={i18n.language}
                  />
                ) : (
                  "—"
                )}
              </div>
            </div>
            <div className="panel stack">
              <h3>{t("entity.directSubclasses")}</h3>
              {childClasses.length === 0 ? (
                <p className="muted">{t("entity.noSubclasses")}</p>
              ) : (
                <div className="stack compact">
                  {childClasses.map((item) => (
                    <EntityLink key={item.id} id={item.id} labels={item.labels} lang={i18n.language} />
                  ))}
                </div>
              )}
            </div>
          </section>
        )}

        {/* ── Instances tab (class only) ── */}
        {tab === "instances" && entity?.classProfile && (
          <section className="stack">
            <div className="row">
              <label className="field">
                {t("search.placeholder")}
                <input value={instanceQuery} onChange={(e) => updateInstanceFilters({ instanceQ: e.target.value })} />
              </label>
              <label className="field">
                {t("entity.subclassFilter")}
                <select value={instanceSubclass} onChange={(e) => updateInstanceFilters({ instanceSubclass: e.target.value })}>
                  <option value="">{t("entity.allSubclasses")}</option>
                  <option value={qid}>{pickLabel(entity.labels, i18n.language, qid)}</option>
                  {descendantClasses.map((item) => (
                    <option key={item.id} value={item.id}>
                      {pickLabel(item.labels, i18n.language, item.id)}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                {t("package.current")}
                <input value={instancePackage} onChange={(e) => updateInstanceFilters({ instancePackage: e.target.value })} />
              </label>
            </div>
            {instanceError && <p className="error">{instanceError}</p>}
            {instanceItems.length === 0 ? (
              <p className="muted">{t("entity.noInstances")}</p>
            ) : (
              <div className="stack compact">
                {instanceItems.map((item) => (
                  <div key={item.id} className="panel">
                    <EntityLink id={item.id} labels={item.labels} lang={i18n.language} />
                    <p className="muted">{item.packageCode || "—"}</p>
                  </div>
                ))}
              </div>
            )}
            {instanceNext && (
              <button type="button" onClick={() => void loadInstances(false, instanceNext)}>
                {t("common.next")}
              </button>
            )}
          </section>
        )}

      </div>
    </div>
  );
}
