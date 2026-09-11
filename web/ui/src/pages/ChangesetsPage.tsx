import { FormEvent, type ReactNode, useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { useChangeSetDraft } from "../changeset";
import { pickLabel } from "../labels";
import { EntityLink, StatementLink } from "../links";
import { ApiValue, displayValueText } from "../ValueEditor";
import { useEntityLookup, type EntitySummary } from "../useEntityLookup";

type ChangeSetItem = {
  objectType: string;
  objectId: string;
  publicId: string;
  op: string;
};

type ChangeSetClaim = {
  objectType: string;
  objectId: string;
  canonicalIri?: string;
  baseRevisionNo?: number;
  opKind?: string;
  publicId?: string;
  packageCode?: string;
  status?: string;
  kind?: string;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
  subject?: string;
  property?: string;
  value?: ApiValue;
  revisionNo?: number;
  updatedAt?: string;
};

type ChangeSetSummary = {
  id: string;
  displayId?: string;
  actor?: string;
  operationType?: string;
  comment?: string;
  status?: string;
  openedAt?: string;
  committedAt?: string;
  correlationId?: string;
  idempotencyKey?: string;
  itemCount?: number;
};

type ChangeSetDetail = ChangeSetSummary & {
  items?: ChangeSetItem[];
  claims?: ChangeSetClaim[];
};

type StatementDetail = {
  id: string;
  displayId?: string;
  subject: string;
  property: string;
  value?: ApiValue;
};

type StatusFilter = "all" | "open" | "committed" | "cancelled";

type Filters = {
  status: StatusFilter;
  q: string;
  actor: string;
  operationType: string;
  objectId: string;
  correlationId: string;
  committedFrom: string;
  committedTo: string;
};

const emptyFilters = (): Filters => ({
  status: "all",
  q: "",
  actor: "",
  operationType: "",
  objectId: "",
  correlationId: "",
  committedFrom: "",
  committedTo: "",
});

const STATUS_OPTIONS: StatusFilter[] = ["all", "open", "committed", "cancelled"];

const ENTITY_OBJECT_TYPES = new Set(["entity", "property", "class"]);

function parseStatus(raw: string | null): StatusFilter {
  if (raw === "open" || raw === "committed" || raw === "cancelled" || raw === "all") return raw;
  return "all";
}

function objectHref(objectType: string, publicId: string): string | null {
  if (!publicId) return null;
  switch (objectType) {
    case "entity":
    case "property":
    case "class":
      return `/entities/${encodeURIComponent(publicId)}`;
    case "statement":
      return `/statements/${encodeURIComponent(publicId)}`;
    case "reference":
      return `/references/${encodeURIComponent(publicId)}`;
    case "package":
      return `/model/packages/${encodeURIComponent(publicId)}`;
    default:
      return null;
  }
}

function entityLabel(entities: Record<string, EntitySummary>, id: string, lang: string): string {
  const ent = entities[id];
  return pickLabel(ent?.labels, lang, ent?.displayId || id);
}

function ChangeSetObjectCell({
  item,
  entities,
  statements,
  lang,
}: {
  item: ChangeSetItem;
  entities: Record<string, EntitySummary>;
  statements: Record<string, StatementDetail>;
  lang: string;
}) {
  const href = objectHref(item.objectType, item.publicId);
  const fallbackId = item.publicId || item.objectId;

  if (ENTITY_OBJECT_TYPES.has(item.objectType) && item.publicId) {
    const ent = entities[item.publicId];
    const typeIds = ent?.effectiveClasses || [];
    const typeLabels = typeIds
      .map((cid) => entityLabel(entities, cid, lang))
      .filter((label, idx, arr) => label && arr.indexOf(label) === idx);
    return (
      <span className="stack compact">
        {typeLabels.length > 0 && <span className="muted">{typeLabels.join(", ")}</span>}
        <EntityLink
          id={item.publicId}
          displayId={ent?.displayId || item.publicId}
          labels={ent?.labels}
          lang={lang}
        />
      </span>
    );
  }

  if (item.objectType === "statement" && item.publicId) {
    const st = statements[item.publicId];
    if (!st) {
      return (
        <StatementLink id={item.publicId} displayId={item.publicId} text={fallbackId} />
      );
    }
    const subject = entities[st.subject];
    const property = entities[st.property];
    let valueNode: ReactNode;
    if (st.value?.type === "EntityReference" && st.value.entityId) {
      const vid = String(st.value.entityId);
      const vent = entities[vid];
      valueNode = (
        <EntityLink id={vid} displayId={vent?.displayId} labels={vent?.labels} lang={lang} />
      );
    } else {
      valueNode = <span>{displayValueText(st.value, lang)}</span>;
    }
    return (
      <span className="stack compact">
        <span className="row compact" style={{ flexWrap: "wrap", alignItems: "baseline", gap: "0.35rem" }}>
          <EntityLink
            id={st.subject}
            displayId={subject?.displayId}
            labels={subject?.labels}
            lang={lang}
          />
          <span className="muted">→</span>
          <EntityLink
            id={st.property}
            displayId={property?.displayId}
            labels={property?.labels}
            lang={lang}
          />
          <span className="muted">→</span>
          {valueNode}
        </span>
        <Link to={`/statements/${encodeURIComponent(st.id)}`}>
          <code className="object-id">{st.displayId || item.publicId}</code>
        </Link>
      </span>
    );
  }

  if (href) {
    return (
      <Link to={href}>
        <code>{fallbackId}</code>
      </Link>
    );
  }
  return <code>{fallbackId}</code>;
}

function claimObjectHref(claim: ChangeSetClaim): string | null {
  const id = claim.publicId || claim.canonicalIri || "";
  if (!id) return null;
  if (claim.objectType === "statement") return `/statements/${encodeURIComponent(id)}`;
  const kind = claim.kind || claim.objectType;
  if (kind === "entity" || kind === "property" || kind === "class" || claim.objectType === "entity") {
    return `/entities/${encodeURIComponent(id)}`;
  }
  return null;
}

function ChangeSetClaimCell({
  claim,
  entities,
  claimLabels,
  lang,
}: {
  claim: ChangeSetClaim;
  entities: Record<string, EntitySummary>;
  claimLabels: Record<string, Record<string, string>>;
  lang: string;
}) {
  const id = claim.publicId || claim.canonicalIri || claim.objectId;
  const labels = claim.labels || claimLabels[id] || entities[id]?.labels;

  if (claim.objectType === "statement") {
    const subjectId = claim.subject || "";
    const propertyId = claim.property || "";
    const subjectLabels = claimLabels[subjectId] || entities[subjectId]?.labels;
    const propertyLabels = claimLabels[propertyId] || entities[propertyId]?.labels;
    let valueNode: ReactNode;
    if (claim.value?.type === "EntityReference" && claim.value.entityId) {
      const vid = String(claim.value.entityId);
      valueNode = (
        <EntityLink
          id={vid}
          displayId={entities[vid]?.displayId || vid}
          labels={claimLabels[vid] || entities[vid]?.labels}
          lang={lang}
        />
      );
    } else {
      valueNode = <span>{displayValueText(claim.value, lang)}</span>;
    }
    return (
      <span className="stack compact">
        <span className="row compact" style={{ flexWrap: "wrap", alignItems: "baseline", gap: "0.35rem" }}>
          {subjectId ? (
            <EntityLink
              id={subjectId}
              displayId={entities[subjectId]?.displayId || subjectId}
              labels={subjectLabels}
              lang={lang}
            />
          ) : (
            <span className="muted">—</span>
          )}
          <span className="muted">→</span>
          {propertyId ? (
            <EntityLink
              id={propertyId}
              displayId={entities[propertyId]?.displayId || propertyId}
              labels={propertyLabels}
              lang={lang}
            />
          ) : (
            <span className="muted">—</span>
          )}
          <span className="muted">→</span>
          {valueNode}
        </span>
        {claim.publicId && (
          <Link to={`/statements/${encodeURIComponent(claim.publicId)}`}>
            <code className="object-id">{claim.publicId}</code>
          </Link>
        )}
        {(claim.status || claim.packageCode) && (
          <span className="muted">
            {[claim.status, claim.packageCode].filter(Boolean).join(" · ")}
          </span>
        )}
      </span>
    );
  }

  const href = claimObjectHref(claim);
  const kindLabel = claim.kind && claim.kind !== "entity" ? claim.kind : null;
  const primaryLabel = pickLabel(labels, lang, "");
  return (
    <span className="stack compact">
      {(kindLabel || claim.status) && (
        <span className="muted">
          {[kindLabel, claim.status].filter(Boolean).join(" · ")}
        </span>
      )}
      {primaryLabel ? (
        <EntityLink id={id} displayId={claim.publicId || id} labels={labels} lang={lang} />
      ) : href ? (
        <Link to={href}>
          <code>{id}</code>
        </Link>
      ) : (
        <code>{id}</code>
      )}
      {claim.descriptions && pickLabel(claim.descriptions, lang, "") && (
        <span className="muted">{pickLabel(claim.descriptions, lang, "")}</span>
      )}
      {claim.packageCode && <span className="muted">{claim.packageCode}</span>}
    </span>
  );
}

function statusLabelKey(status: string): string {
  if (status === "open" || status === "committed" || status === "cancelled") {
    return `changesets.status.${status}`;
  }
  return "changesets.status.committed";
}

function whenStamp(it: ChangeSetSummary): string | undefined {
  if (it.status === "committed") return it.committedAt || it.openedAt;
  return it.openedAt || it.committedAt;
}

export function ChangesetsPage() {
  const { t, i18n } = useTranslation();
  const { cid: rawCid } = useParams();
  const { active, enterChangeSet, updateChangeSet } = useChangeSetDraft();
  const selectedId = (() => {
    if (!rawCid) return "";
    try {
      return decodeURIComponent(rawCid);
    } catch {
      return rawCid;
    }
  })();
  const [params, setParams] = useSearchParams();

  const [filters, setFilters] = useState<Filters>(() => ({
    status: parseStatus(params.get("status")),
    q: params.get("q") || "",
    actor: params.get("actor") || "",
    operationType: params.get("operationType") || "",
    objectId: params.get("objectId") || "",
    correlationId: params.get("correlationId") || "",
    committedFrom: params.get("committedFrom") || "",
    committedTo: params.get("committedTo") || "",
  }));

  const [items, setItems] = useState<ChangeSetSummary[]>([]);
  const [next, setNext] = useState("");
  const [detail, setDetail] = useState<ChangeSetDetail | null>(null);
  const [statements, setStatements] = useState<Record<string, StatementDetail>>({});
  const [error, setError] = useState("");
  const [msg, setMsg] = useState("");
  const [loading, setLoading] = useState(false);
  const [entering, setEntering] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editComment, setEditComment] = useState("");
  const [editOperationType, setEditOperationType] = useState("");

  function syncParams(f: Filters) {
    const sp = new URLSearchParams();
    for (const [k, v] of Object.entries(f)) {
      if (typeof v === "string" && v.trim()) sp.set(k, v.trim());
    }
    setParams(sp, { replace: true });
  }

  async function load(f: Filters, opts?: { append?: boolean; cursor?: string }) {
    setError("");
    setLoading(true);
    const sp = new URLSearchParams();
    sp.set("status", f.status);
    if (f.q.trim()) sp.set("q", f.q.trim());
    if (f.actor.trim()) sp.set("actor", f.actor.trim());
    if (f.operationType.trim()) sp.set("operationType", f.operationType.trim());
    if (f.objectId.trim()) sp.set("objectId", f.objectId.trim());
    if (f.correlationId.trim()) sp.set("correlationId", f.correlationId.trim());
    if (f.committedFrom.trim()) sp.set("committedFrom", toRFC3339(f.committedFrom.trim()));
    if (f.committedTo.trim()) sp.set("committedTo", toRFC3339(f.committedTo.trim(), true));
    if (opts?.cursor) sp.set("cursor", opts.cursor);
    sp.set("limit", "40");
    try {
      const res = await apiFetch<{ items: ChangeSetSummary[]; nextCursor?: string }>(`/v1/changesets?${sp}`);
      setItems((prev) => (opts?.append ? [...prev, ...(res.items || [])] : res.items || []));
      setNext(res.nextCursor || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load(filters);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mount only
  }, []);

  useEffect(() => {
    if (!selectedId) {
      setDetail(null);
      setStatements({});
      setEditComment("");
      setEditOperationType("");
      return;
    }
    let cancelled = false;
    setError("");
    void apiFetch<ChangeSetDetail>(`/v1/changesets/${encodeURIComponent(selectedId)}`)
      .then((res) => {
        if (!cancelled) {
          setDetail(res);
          setStatements({});
          setEditComment(res.comment || "");
          setEditOperationType(res.operationType || "");
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setDetail(null);
          setStatements({});
          setError(err instanceof Error ? err.message : t("common.error"));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selectedId, t]);

  const statementIds = useMemo(() => {
    const ids: string[] = [];
    for (const it of detail?.items || []) {
      if (it.objectType === "statement" && it.publicId) ids.push(it.publicId);
    }
    return ids;
  }, [detail]);

  useEffect(() => {
    if (statementIds.length === 0) {
      setStatements({});
      return;
    }
    let cancelled = false;
    void Promise.all(
      statementIds.map(async (id) => {
        try {
          return await apiFetch<StatementDetail>(`/v1/statements/${encodeURIComponent(id)}`);
        } catch {
          return null;
        }
      }),
    ).then((rows) => {
      if (cancelled) return;
      const nextMap: Record<string, StatementDetail> = {};
      for (const st of rows) {
        if (st?.id) nextMap[st.id] = st;
      }
      setStatements(nextMap);
    });
    return () => {
      cancelled = true;
    };
  }, [statementIds]);

  const relatedEntityIds = useMemo(() => {
    const ids: string[] = [];
    for (const it of detail?.items || []) {
      if (ENTITY_OBJECT_TYPES.has(it.objectType) && it.publicId) ids.push(it.publicId);
    }
    for (const st of Object.values(statements)) {
      ids.push(st.subject, st.property);
      if (st.value?.type === "EntityReference" && st.value.entityId) {
        ids.push(String(st.value.entityId));
      }
    }
    for (const c of detail?.claims || []) {
      if (c.objectType === "entity" && (c.publicId || c.canonicalIri)) {
        ids.push(c.publicId || c.canonicalIri || "");
      }
      if (c.subject) ids.push(c.subject);
      if (c.property) ids.push(c.property);
      if (c.value?.type === "EntityReference" && c.value.entityId) {
        ids.push(String(c.value.entityId));
      }
    }
    return ids.filter(Boolean);
  }, [detail, statements]);

  const claimLabelMap = useMemo(() => {
    const map: Record<string, Record<string, string>> = {};
    for (const c of detail?.claims || []) {
      if (c.objectType !== "entity") continue;
      const id = c.publicId || c.canonicalIri;
      if (id && c.labels) map[id] = c.labels;
    }
    return map;
  }, [detail]);

  const entities = useEntityLookup(relatedEntityIds);

  const classIds = useMemo(() => {
    const ids: string[] = [];
    for (const id of relatedEntityIds) {
      for (const cid of entities[id]?.effectiveClasses || []) ids.push(cid);
    }
    return ids;
  }, [relatedEntityIds, entities]);

  const entitiesWithClasses = useEntityLookup([...relatedEntityIds, ...classIds]);

  function onFilter(e: FormEvent) {
    e.preventDefault();
    syncParams(filters);
    void load(filters);
  }

  function setStatusFilter(status: StatusFilter) {
    const nextFilters = { ...filters, status };
    setFilters(nextFilters);
    syncParams(nextFilters);
    void load(nextFilters);
  }

  function clearFilters() {
    const empty = emptyFilters();
    setFilters(empty);
    setParams({}, { replace: true });
    void load(empty);
  }

  async function onEnter() {
    if (!detail || detail.status !== "open") return;
    if (active?.id && active.id !== detail.id) {
      const ok = window.confirm(t("changeset.enterOtherWarn", { id: active.id }));
      if (!ok) return;
    }
    setEntering(true);
    setError("");
    setMsg("");
    try {
      await enterChangeSet(detail.id);
      setMsg(t("changeset.entered"));
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setEntering(false);
    }
  }

  async function onSaveDetails(e: FormEvent) {
    e.preventDefault();
    if (!detail || detail.status !== "open") return;
    setSaving(true);
    setError("");
    setMsg("");
    try {
      const updated = await updateChangeSet(detail.id, {
        comment: editComment,
        operationType: editOperationType,
      });
      setDetail((prev) =>
        prev
          ? {
              ...prev,
              comment: updated.comment ?? editComment,
              operationType: updated.operationType ?? editOperationType,
            }
          : prev,
      );
      setItems((prev) =>
        prev.map((it) =>
          it.id === detail.id
            ? {
                ...it,
                comment: updated.comment ?? editComment,
                operationType: updated.operationType ?? editOperationType,
              }
            : it,
        ),
      );
      setMsg(t("changeset.detailsSaved"));
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setSaving(false);
    }
  }

  const fmt = new Intl.DateTimeFormat(i18n.language || undefined, {
    dateStyle: "short",
    timeStyle: "medium",
  });

  const querySuffix = params.toString() ? `?${params}` : "";
  const lang = i18n.language || "en";
  const isOpenDetail = detail?.status === "open";
  const isActiveDetail = !!detail && active?.id === detail.id;

  return (
    <div className="stack">
      <h1>{t("changesets.title")}</h1>
      <p className="muted">{t("changesets.subtitle")}</p>

      <form className="panel stack" onSubmit={onFilter}>
        <div className="toolbar wrap" role="group" aria-label={t("changesets.filter.status")}>
          {STATUS_OPTIONS.map((status) => {
            const labelKey =
              status === "all"
                ? "changesets.filter.statusAll"
                : status === "open"
                  ? "changesets.filter.statusOpen"
                  : status === "committed"
                    ? "changesets.filter.statusCommitted"
                    : "changesets.filter.statusCancelled";
            return (
              <button
                key={status}
                type="button"
                className={filters.status === status ? "primary" : undefined}
                onClick={() => setStatusFilter(status)}
              >
                {t(labelKey)}
              </button>
            );
          })}
        </div>
        <div className="toolbar wrap">
          <label className="field grow">
            {t("changesets.filter.q")}
            <input
              value={filters.q}
              onChange={(e) => setFilters((f) => ({ ...f, q: e.target.value }))}
              placeholder={t("changesets.filter.qPh")}
            />
          </label>
          <label className="field">
            {t("changesets.filter.actor")}
            <input value={filters.actor} onChange={(e) => setFilters((f) => ({ ...f, actor: e.target.value }))} />
          </label>
          <label className="field">
            {t("changesets.filter.operationType")}
            <input
              value={filters.operationType}
              onChange={(e) => setFilters((f) => ({ ...f, operationType: e.target.value }))}
              placeholder="batch, draftCommit…"
            />
          </label>
        </div>
        <div className="toolbar wrap">
          <label className="field">
            {t("changesets.filter.objectId")}
            <input
              value={filters.objectId}
              onChange={(e) => setFilters((f) => ({ ...f, objectId: e.target.value }))}
              placeholder="Q… / S… / P…"
            />
          </label>
          <label className="field">
            {t("changesets.filter.correlationId")}
            <input
              value={filters.correlationId}
              onChange={(e) => setFilters((f) => ({ ...f, correlationId: e.target.value }))}
            />
          </label>
          <label className="field">
            {t("changesets.filter.from")}
            <input
              type="datetime-local"
              value={filters.committedFrom}
              onChange={(e) => setFilters((f) => ({ ...f, committedFrom: e.target.value }))}
            />
          </label>
          <label className="field">
            {t("changesets.filter.to")}
            <input
              type="datetime-local"
              value={filters.committedTo}
              onChange={(e) => setFilters((f) => ({ ...f, committedTo: e.target.value }))}
            />
          </label>
        </div>
        <div className="toolbar">
          <button type="submit" className="primary" disabled={loading}>
            {t("common.filter")}
          </button>
          <button type="button" onClick={clearFilters}>
            {t("common.clear")}
          </button>
        </div>
      </form>

      {error && <p className="error">{error}</p>}
      {msg && <p className="muted">{msg}</p>}

      <div className="changeset-browser">
        <div className="panel stack compact">
          <h2>{t("changesets.list")}</h2>
          {items.length === 0 && !loading && <p className="muted">{t("changesets.empty")}</p>}
          <table className="table">
            <thead>
              <tr>
                <th>{t("changesets.col.id")}</th>
                <th>{t("changesets.col.status")}</th>
                <th>{t("changesets.col.when")}</th>
                <th>{t("changesets.col.actor")}</th>
                <th>{t("changesets.col.type")}</th>
                <th>{t("changesets.col.items")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => {
                const rowActive = it.id === selectedId;
                const status = it.status || "committed";
                const stamp = whenStamp(it);
                return (
                  <tr key={it.id} className={rowActive ? "row-active" : undefined}>
                    <td>
                      <Link to={`/changesets/${encodeURIComponent(it.id)}${querySuffix}`}>
                        <code>{it.displayId || it.id}</code>
                      </Link>
                      {active?.id === it.id && (
                        <span className="pill" style={{ marginLeft: "0.4rem" }}>
                          {t("changesets.activeBadge")}
                        </span>
                      )}
                    </td>
                    <td>
                      <span className={`pill status-${status}`}>{t(statusLabelKey(status))}</span>
                    </td>
                    <td className="muted">{stamp ? fmt.format(new Date(stamp)) : "—"}</td>
                    <td>{it.actor || "—"}</td>
                    <td>
                      <code>{it.operationType || "—"}</code>
                    </td>
                    <td>{it.itemCount ?? 0}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {next && (
            <button type="button" disabled={loading} onClick={() => void load(filters, { append: true, cursor: next })}>
              {t("common.next")}
            </button>
          )}
        </div>

        <div className="panel stack compact">
          <h2>{t("changesets.detail")}</h2>
          {!selectedId && <p className="muted">{t("changesets.select")}</p>}
          {selectedId && detail && (
            <>
              <div className="toolbar wrap">
                {isOpenDetail && (
                  <button
                    type="button"
                    className="primary"
                    disabled={entering || isActiveDetail}
                    onClick={() => void onEnter()}
                  >
                    {isActiveDetail ? t("changesets.activeBadge") : t("changeset.enter")}
                  </button>
                )}
                {detail.status && (
                  <span className={`pill status-${detail.status}`}>{t(statusLabelKey(detail.status))}</span>
                )}
              </div>

              <dl className="meta-grid">
                <dt>{t("changesets.col.id")}</dt>
                <dd>
                  <code>{detail.displayId || detail.id}</code>
                </dd>
                <dt>{t("changesets.col.status")}</dt>
                <dd>{t(statusLabelKey(detail.status || "committed"))}</dd>
                <dt>{t("changesets.col.opened")}</dt>
                <dd>{detail.openedAt ? fmt.format(new Date(detail.openedAt)) : "—"}</dd>
                <dt>{t("changesets.col.committed")}</dt>
                <dd>{detail.committedAt ? fmt.format(new Date(detail.committedAt)) : "—"}</dd>
                <dt>{t("changesets.col.actor")}</dt>
                <dd>{detail.actor || "—"}</dd>
                {!isOpenDetail && (
                  <>
                    <dt>{t("changesets.col.type")}</dt>
                    <dd>
                      <code>{detail.operationType || "—"}</code>
                    </dd>
                    {detail.comment && (
                      <>
                        <dt>{t("changesets.col.comment")}</dt>
                        <dd>{detail.comment}</dd>
                      </>
                    )}
                  </>
                )}
                {detail.correlationId && (
                  <>
                    <dt>{t("changesets.col.correlationId")}</dt>
                    <dd>
                      <code>{detail.correlationId}</code>
                    </dd>
                  </>
                )}
                {detail.idempotencyKey && (
                  <>
                    <dt>{t("changesets.col.idempotencyKey")}</dt>
                    <dd>
                      <code>{detail.idempotencyKey}</code>
                    </dd>
                  </>
                )}
              </dl>

              {isOpenDetail && (
                <form className="stack compact" onSubmit={(e) => void onSaveDetails(e)}>
                  <h3>{t("changesets.editDetails")}</h3>
                  <label className="field">
                    {t("changesets.col.comment")}
                    <textarea
                      value={editComment}
                      onChange={(e) => setEditComment(e.target.value)}
                      placeholder={t("changesets.commentPh")}
                      rows={3}
                    />
                  </label>
                  <label className="field">
                    {t("changesets.col.type")}
                    <input
                      value={editOperationType}
                      onChange={(e) => setEditOperationType(e.target.value)}
                      placeholder={t("changesets.operationTypePh")}
                    />
                  </label>
                  <div className="toolbar">
                    <button type="submit" className="primary" disabled={saving}>
                      {t("changeset.saveDetails")}
                    </button>
                  </div>
                </form>
              )}

              {isOpenDetail ? (
                <>
                  <h3>{t("changesets.claims")}</h3>
                  {(detail.claims || []).length === 0 ? (
                    <p className="muted">{t("changesets.noClaims")}</p>
                  ) : (
                    <table className="table">
                      <thead>
                        <tr>
                          <th>{t("changesets.col.op")}</th>
                          <th>{t("changesets.col.objectType")}</th>
                          <th>{t("changesets.col.summary")}</th>
                          <th>{t("changesets.col.baseRevision")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(detail.claims || []).map((c) => (
                          <tr key={`${c.objectId}-${c.opKind}`}>
                            <td>
                              <code>{c.opKind || "—"}</code>
                              {c.revisionNo != null && c.revisionNo > 0 && (
                                <div className="muted" style={{ fontSize: "0.85em" }}>
                                  {t("changesets.col.revision")} {c.revisionNo}
                                </div>
                              )}
                            </td>
                            <td>
                              {c.kind && c.kind !== c.objectType ? `${c.objectType}/${c.kind}` : c.objectType}
                            </td>
                            <td>
                              <ChangeSetClaimCell
                                claim={c}
                                entities={entitiesWithClasses}
                                claimLabels={claimLabelMap}
                                lang={lang}
                              />
                            </td>
                            <td>{c.baseRevisionNo ?? "—"}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </>
              ) : (
                <>
                  <h3>{t("changesets.items")}</h3>
                  {(detail.items || []).length === 0 ? (
                    <p className="muted">{t("changesets.noItems")}</p>
                  ) : (
                    <table className="table">
                      <thead>
                        <tr>
                          <th>{t("changesets.col.op")}</th>
                          <th>{t("changesets.col.objectType")}</th>
                          <th>{t("changesets.col.summary")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(detail.items || []).map((it, idx) => (
                          <tr key={`${it.objectId}-${idx}`}>
                            <td>
                              <code>{it.op}</code>
                            </td>
                            <td>{it.objectType}</td>
                            <td>
                              <ChangeSetObjectCell
                                item={it}
                                entities={entitiesWithClasses}
                                statements={statements}
                                lang={lang}
                              />
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function toRFC3339(local: string, endOfMinute = false): string {
  if (!local) return local;
  if (local.includes("T") && local.length === 16) {
    const d = new Date(local);
    if (Number.isNaN(d.getTime())) return local;
    if (endOfMinute) d.setSeconds(59, 999);
    return d.toISOString();
  }
  return local;
}
