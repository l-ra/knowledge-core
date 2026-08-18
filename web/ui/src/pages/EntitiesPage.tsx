import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { pickLabel } from "../labels";
import { usePackage } from "../package";
import { useChangeSetDraft } from "../changeset";
import { EntityLink } from "../links";

type Entity = {
  id: string;
  displayId?: string;
  kind?: string;
  labels?: Record<string, string>;
  status?: string;
  packageCode?: string;
};

type KindFilter = "" | "entity" | "property" | "class";

const DATATYPES = [
  "String",
  "EntityReference",
  "Boolean",
  "Integer",
  "Decimal",
  "Date",
  "DateTime",
  "URI",
  "LocalizedString",
  "ExternalIdentifier",
  "Quantity",
  "Interval",
  "Any",
];

export function EntitiesPage() {
  const { t, i18n } = useTranslation();
  const [params, setParams] = useSearchParams();
  const kind = (params.get("kind") as KindFilter) || "";
  const packageFilter = params.get("package") || "";
  const nav = useNavigate();
  const { packageCode, packages } = usePackage();
  const { isOpen, runWrite } = useChangeSetDraft();

  const [items, setItems] = useState<Entity[]>([]);
  const [q, setQ] = useState(params.get("q") || "");
  const [next, setNext] = useState("");
  const [error, setError] = useState("");
  const [info, setInfo] = useState("");
  const [creating, setCreating] = useState<KindFilter | null>(null);

  // create form state
  const [label, setLabel] = useState("");
  const [iriLocal, setIriLocal] = useState("");
  const [datatype, setDatatype] = useState("String");
  const [constraintsJSON, setConstraintsJSON] = useState("{}");
  const [subClassOf, setSubClassOf] = useState("");

  async function load(opts?: { reset?: boolean; cursor?: string }) {
    setError("");
    const sp = new URLSearchParams();
    if (q) sp.set("q", q);
    if (kind) sp.set("kind", kind);
    if (packageFilter) sp.set("package", packageFilter);
    if (opts?.cursor) sp.set("cursor", opts.cursor);
    sp.set("limit", "30");
    try {
      const res = await apiFetch<{ items: Entity[]; nextCursor?: string }>(`/v1/entities?${sp}`);
      setItems((prev) => (opts?.reset ? res.items : [...prev, ...res.items]));
      setNext(res.nextCursor || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load({ reset: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind, packageFilter]);

  function setKind(k: KindFilter) {
    const nextParams = new URLSearchParams(params);
    if (k) nextParams.set("kind", k);
    else nextParams.delete("kind");
    setParams(nextParams);
    setCreating(null);
  }

  function toggleCurrentPackageOnly() {
    const nextParams = new URLSearchParams(params);
    if (packageFilter && packageFilter === packageCode) {
      nextParams.delete("package");
    } else if (packageCode) {
      nextParams.set("package", packageCode);
    }
    setParams(nextParams);
  }

  function openEntity(id: string) {
    nav(`/entities/${encodeURIComponent(id)}`);
  }

  async function create(e: FormEvent) {
    e.preventDefault();
    if (!packageCode) {
      setError(t("package.required"));
      return;
    }
    if (!creating) return;
    setInfo("");
    try {
      if (creating === "entity") {
        await runWrite(
          async () => {
            const res = await apiFetch<{ data: Entity }>("/v1/entities", {
              method: "POST",
              body: JSON.stringify({
                packageCode,
                labels: { en: label },
                iriLocal: iriLocal || undefined,
              }),
            });
            openEntity(res.data.id);
          },
          {
            op: "createEntity",
            packageCode,
            labels: { en: label },
            iriLocal: iriLocal || undefined,
          },
        );
      } else if (creating === "property") {
        let constraints: Record<string, unknown> = {};
        try {
          constraints = JSON.parse(constraintsJSON);
        } catch {
          throw new Error("invalid constraints JSON");
        }
        await runWrite(
          async () => {
            const res = await apiFetch<{ data: Entity }>("/v1/properties", {
              method: "POST",
              body: JSON.stringify({
                packageCode,
                datatype,
                labels: { en: label },
                constraints,
                iriLocal: iriLocal || undefined,
              }),
            });
            openEntity(res.data.id);
          },
          {
            op: "createProperty",
            packageCode,
            datatype,
            labels: { en: label },
            constraints,
            iriLocal: iriLocal || undefined,
          },
        );
      } else if (creating === "class") {
        await runWrite(
          async () => {
            const res = await apiFetch<{ data: Entity }>("/v1/classes", {
              method: "POST",
              body: JSON.stringify({
                packageCode,
                labels: { en: label },
                subClassOf: subClassOf || undefined,
                iriLocal: iriLocal || undefined,
              }),
            });
            openEntity(res.data.id);
          },
          {
            op: "createClass",
            packageCode,
            labels: { en: label },
            subClassOf: subClassOf || undefined,
            iriLocal: iriLocal || undefined,
          },
        );
      }
      if (isOpen) {
        setInfo(t("changeset.queued"));
        setCreating(null);
        setLabel("");
        setIriLocal("");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  const showAddEntity = !kind || kind === "entity";
  const showAddProperty = !kind || kind === "property";
  const showAddClass = !kind || kind === "class";
  const packageOnly = !!packageFilter && packageFilter === packageCode;

  return (
    <div className="stack">
      <div className="row" style={{ alignItems: "baseline", justifyContent: "space-between" }}>
        <h1>{t("entities.title")}</h1>
        <div className="row">
          {showAddEntity && (
            <button type="button" className="primary" disabled={!packageCode} onClick={() => setCreating("entity")}>
              {t("entities.create")}
            </button>
          )}
          {showAddProperty && (
            <button type="button" className="primary" disabled={!packageCode} onClick={() => setCreating("property")}>
              {t("properties.create")}
            </button>
          )}
          {showAddClass && (
            <button type="button" className="primary" disabled={!packageCode} onClick={() => setCreating("class")}>
              {t("classes.create")}
            </button>
          )}
        </div>
      </div>

      {!packageCode && packages.length === 0 && <p className="error">{t("package.none")}</p>}
      {!packageCode && packages.length > 0 && <p className="muted">{t("package.required")}</p>}

      <div className="toolbar kind-filters">
        {(
          [
            ["", t("entities.kindAll")],
            ["entity", t("entities.kindEntity")],
            ["property", t("entities.kindProperty")],
            ["class", t("entities.kindClass")],
          ] as const
        ).map(([k, labelText]) => (
          <button key={k || "all"} type="button" className={kind === k ? "primary" : undefined} onClick={() => setKind(k)}>
            {labelText}
          </button>
        ))}
        <label className="row" style={{ gap: "0.4rem" }}>
          <input
            type="checkbox"
            checked={packageOnly}
            disabled={!packageCode}
            onChange={() => toggleCurrentPackageOnly()}
          />
          {t("entities.currentPackageOnly")}
        </label>
      </div>

      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault();
          const nextParams = new URLSearchParams(params);
          if (q) nextParams.set("q", q);
          else nextParams.delete("q");
          setParams(nextParams);
          void load({ reset: true });
        }}
      >
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("search.placeholder")} />
        <button className="primary">{t("search.submit")}</button>
      </form>

      {creating && (
        <form className="panel stack" onSubmit={create}>
          <h2>
            {creating === "entity" && t("entities.create")}
            {creating === "property" && t("properties.create")}
            {creating === "class" && t("classes.create")}
          </h2>
          <label className="field">
            {t("entities.labelEn")}
            <input value={label} onChange={(e) => setLabel(e.target.value)} required />
          </label>
          <label className="field">
            {t("entity.iriLocal")}
            <input
              value={iriLocal}
              onChange={(e) => setIriLocal(e.target.value)}
              placeholder={t("entity.iriLocalPlaceholder")}
            />
            <span className="muted">{t("entity.iriLocalHint")}</span>
          </label>
          {creating === "property" && (
            <>
              <label className="field">
                {t("properties.datatype")}
                <select value={datatype} onChange={(e) => setDatatype(e.target.value)}>
                  {DATATYPES.map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                {t("properties.constraints")}
                <textarea rows={3} value={constraintsJSON} onChange={(e) => setConstraintsJSON(e.target.value)} />
              </label>
            </>
          )}
          {creating === "class" && (
            <label className="field">
              {t("classes.subClassOf")}
              <input value={subClassOf} onChange={(e) => setSubClassOf(e.target.value)} placeholder="C1" />
            </label>
          )}
          <div className="row">
            <button className="primary" type="submit" disabled={!packageCode}>
              {t("common.save")}
            </button>
            <button type="button" onClick={() => setCreating(null)}>
              {t("common.cancel")}
            </button>
          </div>
        </form>
      )}

      {info && <p className="muted">{info}</p>}
      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>{t("entity.label")}</th>
            <th>{t("entities.kind")}</th>
            <th>Package</th>
          </tr>
        </thead>
        <tbody>
          {items.map((it) => (
            <tr key={it.id}>
              <td>
                <EntityLink id={it.id} displayId={it.displayId} labels={it.labels} lang={i18n.language} />
              </td>
              <td>{it.kind || "entity"}</td>
              <td>{it.packageCode || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {next && (
        <button type="button" onClick={() => void load({ cursor: next })}>
          {t("common.next")}
        </button>
      )}
    </div>
  );
}
