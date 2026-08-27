import { FormEvent, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type ChangeSetItem = {
  objectType: string;
  objectId: string;
  publicId: string;
  op: string;
};

type ChangeSetSummary = {
  id: string;
  displayId?: string;
  actor?: string;
  operationType?: string;
  comment?: string;
  committedAt?: string;
  correlationId?: string;
  idempotencyKey?: string;
  itemCount?: number;
};

type ChangeSetDetail = ChangeSetSummary & {
  items?: ChangeSetItem[];
};

type Filters = {
  q: string;
  actor: string;
  operationType: string;
  objectId: string;
  correlationId: string;
  committedFrom: string;
  committedTo: string;
};

const emptyFilters = (): Filters => ({
  q: "",
  actor: "",
  operationType: "",
  objectId: "",
  correlationId: "",
  committedFrom: "",
  committedTo: "",
});

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

export function ChangesetsPage() {
  const { t, i18n } = useTranslation();
  const { cid: rawCid } = useParams();
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
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  function syncParams(f: Filters) {
    const sp = new URLSearchParams();
    for (const [k, v] of Object.entries(f)) {
      if (v.trim()) sp.set(k, v.trim());
    }
    setParams(sp, { replace: true });
  }

  async function load(f: Filters, opts?: { append?: boolean; cursor?: string }) {
    setError("");
    setLoading(true);
    const sp = new URLSearchParams();
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
      return;
    }
    let cancelled = false;
    setError("");
    void apiFetch<ChangeSetDetail>(`/v1/changesets/${encodeURIComponent(selectedId)}`)
      .then((res) => {
        if (!cancelled) setDetail(res);
      })
      .catch((err) => {
        if (!cancelled) {
          setDetail(null);
          setError(err instanceof Error ? err.message : t("common.error"));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selectedId, t]);

  function onFilter(e: FormEvent) {
    e.preventDefault();
    syncParams(filters);
    void load(filters);
  }

  function clearFilters() {
    const empty = emptyFilters();
    setFilters(empty);
    setParams({}, { replace: true });
    void load(empty);
  }

  const fmt = new Intl.DateTimeFormat(i18n.language || undefined, {
    dateStyle: "short",
    timeStyle: "medium",
  });

  const querySuffix = params.toString() ? `?${params}` : "";

  return (
    <div className="stack">
      <h1>{t("changesets.title")}</h1>
      <p className="muted">{t("changesets.subtitle")}</p>

      <form className="panel stack" onSubmit={onFilter}>
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

      <div className="changeset-browser">
        <div className="panel stack compact">
          <h2>{t("changesets.list")}</h2>
          {items.length === 0 && !loading && <p className="muted">{t("changesets.empty")}</p>}
          <table className="table">
            <thead>
              <tr>
                <th>{t("changesets.col.id")}</th>
                <th>{t("changesets.col.committed")}</th>
                <th>{t("changesets.col.actor")}</th>
                <th>{t("changesets.col.type")}</th>
                <th>{t("changesets.col.items")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => {
                const active = it.id === selectedId;
                return (
                  <tr key={it.id} className={active ? "row-active" : undefined}>
                    <td>
                      <Link to={`/changesets/${encodeURIComponent(it.id)}${querySuffix}`}>
                        <code>{it.displayId || it.id}</code>
                      </Link>
                    </td>
                    <td className="muted">{it.committedAt ? fmt.format(new Date(it.committedAt)) : "—"}</td>
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
              <dl className="meta-grid">
                <dt>{t("changesets.col.id")}</dt>
                <dd>
                  <code>{detail.displayId || detail.id}</code>
                </dd>
                <dt>{t("changesets.col.committed")}</dt>
                <dd>{detail.committedAt ? fmt.format(new Date(detail.committedAt)) : "—"}</dd>
                <dt>{t("changesets.col.actor")}</dt>
                <dd>{detail.actor || "—"}</dd>
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
              <h3>{t("changesets.items")}</h3>
              {(detail.items || []).length === 0 ? (
                <p className="muted">{t("changesets.noItems")}</p>
              ) : (
                <table className="table">
                  <thead>
                    <tr>
                      <th>{t("changesets.col.op")}</th>
                      <th>{t("changesets.col.objectType")}</th>
                      <th>{t("changesets.col.object")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(detail.items || []).map((it, idx) => {
                      const href = objectHref(it.objectType, it.publicId);
                      return (
                        <tr key={`${it.objectId}-${idx}`}>
                          <td>
                            <code>{it.op}</code>
                          </td>
                          <td>{it.objectType}</td>
                          <td>
                            {href ? (
                              <Link to={href}>
                                <code>{it.publicId || it.objectId}</code>
                              </Link>
                            ) : (
                              <code>{it.publicId || it.objectId}</code>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
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
