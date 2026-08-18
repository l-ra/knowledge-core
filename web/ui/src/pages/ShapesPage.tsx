import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { pickLabel } from "../labels";
import { EntityLink } from "../links";

type Shape = {
  code: string;
  classId: string;
  document?: {
    requiredProperties?: string[];
    allowedProperties?: string[];
    closed?: boolean;
    severity?: string;
  };
};

type ClassDef = { id: string; displayId?: string; labels?: Record<string, string> };
type PropDef = { id: string; displayId?: string; labels?: Record<string, string> };

export function ShapesPage() {
  const { t, i18n } = useTranslation();
  const [items, setItems] = useState<Shape[]>([]);
  const [classes, setClasses] = useState<ClassDef[]>([]);
  const [properties, setProperties] = useState<PropDef[]>([]);
  const [code, setCode] = useState("");
  const [classId, setClassId] = useState("");
  const [required, setRequired] = useState("");
  const [closed, setClosed] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    try {
      const [shapes, cls, props] = await Promise.all([
        apiFetch<{ items: Shape[] }>("/v1/shapes"),
        apiFetch<{ items: ClassDef[] }>("/v1/classes?limit=200"),
        apiFetch<{ items: PropDef[] }>("/v1/properties?limit=500"),
      ]);
      setItems(shapes.items || []);
      setClasses(cls.items || []);
      setProperties(props.items || []);
      if (!classId && cls.items?.[0]) setClassId(cls.items[0].id);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function create(e: FormEvent) {
    e.preventDefault();
    try {
      const requiredProperties = required
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      await apiFetch("/v1/shapes", {
        method: "POST",
        body: JSON.stringify({
          code,
          classId,
          document: { requiredProperties, closed, severity: "error" },
        }),
      });
      setCode("");
      setRequired("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  const classById = new Map(classes.map((c) => [c.id, c]));
  const propById = new Map(properties.map((p) => [p.id, p]));

  return (
    <div className="stack">
      <h1>{t("shapes.title")}</h1>
      <form className="panel stack" onSubmit={create}>
        <div className="row">
          <label className="field">
            {t("shapes.code")}
            <input value={code} onChange={(e) => setCode(e.target.value)} required />
          </label>
          <label className="field">
            {t("shapes.class")}
            <select value={classId} onChange={(e) => setClassId(e.target.value)}>
              {classes.map((c) => (
                <option key={c.id} value={c.id}>
                  {pickLabel(c.labels, i18n.language, c.id)} ({c.displayId || c.id})
                </option>
              ))}
            </select>
          </label>
        </div>
        <label className="field">
          {t("shapes.requiredProperties")}
          <input value={required} onChange={(e) => setRequired(e.target.value)} placeholder="P1, P2" />
        </label>
        <label className="field">
          <input type="checkbox" checked={closed} onChange={(e) => setClosed(e.target.checked)} />{" "}
          {t("shapes.closed")}
        </label>
        <button className="primary">{t("shapes.create")}</button>
      </form>
      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>{t("shapes.code")}</th>
            <th>{t("shapes.class")}</th>
            <th>{t("shapes.requiredProperties")}</th>
            <th>{t("shapes.closed")}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((s) => (
            <tr key={s.code}>
              <td>{s.code}</td>
              <td>
                <EntityLink
                  id={s.classId}
                  displayId={classById.get(s.classId)?.displayId}
                  labels={classById.get(s.classId)?.labels}
                  lang={i18n.language}
                />
              </td>
              <td>
                {(s.document?.requiredProperties || []).length > 0
                  ? s.document!.requiredProperties!.map((pid, i) => (
                      <span key={pid}>
                        {i > 0 && ", "}
                        <EntityLink
                          id={pid}
                          displayId={propById.get(pid)?.displayId}
                          labels={propById.get(pid)?.labels}
                          lang={i18n.language}
                        />
                      </span>
                    ))
                  : "—"}
              </td>
              <td>{s.document?.closed ? "yes" : "no"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="muted">
        <Link to="/model/classes">{t("nav.classes")}</Link>
      </p>
    </div>
  );
}
