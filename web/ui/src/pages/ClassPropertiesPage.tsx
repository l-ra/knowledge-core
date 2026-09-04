import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { pickLabel } from "../labels";
import { EntityLink } from "../links";
import { usePackage } from "../package";

type ClassDef = {
  id: string;
  displayId?: string;
  packageCode?: string;
  iriLocal?: string;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
};

type PropDef = {
  id: string;
  displayId?: string;
  packageCode?: string;
  iriLocal?: string;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
  datatype?: string;
  constraints?: {
    domainClasses?: string[];
    rangeClasses?: string[];
  };
};

type Shape = {
  code: string;
  classId: string;
  packageCode?: string;
  document?: {
    requiredProperties?: string[];
    allowedProperties?: string[];
  };
};

type PropertyRow = {
  property: PropDef;
  required: boolean;
};

type ClassBlock = {
  cls: ClassDef;
  properties: PropertyRow[];
};

async function fetchAllPages<T>(basePath: string): Promise<T[]> {
  const items: T[] = [];
  let cursor = "";
  for (;;) {
    const sep = basePath.includes("?") ? "&" : "?";
    const url = cursor
      ? `${basePath}${sep}cursor=${encodeURIComponent(cursor)}&limit=200`
      : `${basePath}${sep}limit=200`;
    const res = await apiFetch<{ items: T[]; nextCursor?: string }>(url);
    items.push(...(res.items || []));
    if (!res.nextCursor) break;
    cursor = res.nextCursor;
  }
  return items;
}

function uriNamespacePath(iriLocal: string): string {
  const slash = iriLocal.lastIndexOf("/");
  if (slash <= 0) return "";
  return iriLocal.slice(0, slash + 1);
}

function formatValueType(prop: PropDef, lang: string, classById: Map<string, ClassDef>): string {
  const dt = prop.datatype || "—";
  if (dt !== "EntityReference") return dt;
  const ranges = prop.constraints?.rangeClasses || [];
  if (ranges.length === 0) return dt;
  const labels = ranges
    .map((id) => pickLabel(classById.get(id)?.labels, lang, classById.get(id)?.displayId || id))
    .join(", ");
  return `${dt} → ${labels}`;
}

function searchableText(parts: Array<string | undefined>): string {
  return parts.filter(Boolean).join(" ").toLowerCase();
}

function buildClassProperties(
  classes: ClassDef[],
  properties: PropDef[],
  shapes: Shape[],
): Map<string, PropertyRow[]> {
  const propById = new Map(properties.map((p) => [p.id, p]));
  const shapeByClass = new Map(shapes.map((s) => [s.classId, s]));
  const propIdsByClass = new Map<string, Set<string>>();

  for (const prop of properties) {
    for (const classId of prop.constraints?.domainClasses || []) {
      if (!propIdsByClass.has(classId)) propIdsByClass.set(classId, new Set());
      propIdsByClass.get(classId)!.add(prop.id);
    }
  }

  for (const shape of shapes) {
    const ids = [
      ...(shape.document?.requiredProperties || []),
      ...(shape.document?.allowedProperties || []),
    ];
    if (!propIdsByClass.has(shape.classId)) propIdsByClass.set(shape.classId, new Set());
    for (const pid of ids) propIdsByClass.get(shape.classId)!.add(pid);
  }

  const requiredByClass = new Map<string, Set<string>>();
  for (const shape of shapes) {
    const req = new Set(shape.document?.requiredProperties || []);
    if (req.size === 0) continue;
    const existing = requiredByClass.get(shape.classId) || new Set<string>();
    for (const pid of req) existing.add(pid);
    requiredByClass.set(shape.classId, existing);
  }

  const result = new Map<string, PropertyRow[]>();
  for (const cls of classes) {
    const ids = propIdsByClass.get(cls.id);
    if (!ids || ids.size === 0) continue;
    const required = requiredByClass.get(cls.id) || new Set<string>();
    const rows = Array.from(ids)
      .map((pid) => propById.get(pid))
      .filter((p): p is PropDef => Boolean(p))
      .map((property) => ({ property, required: required.has(property.id) }))
      .sort((a, b) =>
        pickLabel(a.property.labels, "en", a.property.displayId || a.property.id).localeCompare(
          pickLabel(b.property.labels, "en", b.property.displayId || b.property.id),
          undefined,
          { sensitivity: "base" },
        ),
      );
    result.set(cls.id, rows);
  }
  return result;
}

export function ClassPropertiesPage() {
  const { t, i18n } = useTranslation();
  const { packages } = usePackage();
  const [classes, setClasses] = useState<ClassDef[]>([]);
  const [properties, setProperties] = useState<PropDef[]>([]);
  const [shapes, setShapes] = useState<Shape[]>([]);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setLoading(true);
      setError("");
      try {
        const [cls, props, sh] = await Promise.all([
          fetchAllPages<ClassDef>("/v1/classes"),
          fetchAllPages<PropDef>("/v1/properties"),
          apiFetch<{ items: Shape[] }>("/v1/shapes"),
        ]);
        if (cancelled) return;
        setClasses(cls);
        setProperties(props);
        setShapes(sh.items || []);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : t("common.error"));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [t]);

  const classById = useMemo(() => new Map(classes.map((c) => [c.id, c])), [classes]);
  const propsByClass = useMemo(
    () => buildClassProperties(classes, properties, shapes),
    [classes, properties, shapes],
  );

  const grouped = useMemo(() => {
    const q = query.trim().toLowerCase();
    const packageOrder = packages.map((p) => p.code);
    const packageLabels = new Map(packages.map((p) => [p.code, p.labels]));

    type NamespaceGroup = { path: string; classes: ClassBlock[] };
    type PackageGroup = {
      code: string;
      packageLabels?: Record<string, string>;
      namespaces: NamespaceGroup[];
    };

    const byPackage = new Map<string, Map<string, ClassBlock[]>>();

    for (const cls of classes) {
      const rows = propsByClass.get(cls.id);
      if (!rows) continue;

      const classText = searchableText([
        cls.id,
        cls.displayId,
        cls.iriLocal,
        cls.packageCode,
        pickLabel(cls.labels, i18n.language, ""),
        pickLabel(cls.descriptions, i18n.language, ""),
      ]);

      const rowTexts = rows.map(({ property, required }) =>
        searchableText([
          property.id,
          property.displayId,
          property.iriLocal,
          property.datatype,
          pickLabel(property.labels, i18n.language, ""),
          pickLabel(property.descriptions, i18n.language, ""),
          required ? "required" : "",
        ]),
      );

      if (q && !classText.includes(q) && !rowTexts.some((text) => text.includes(q))) {
        continue;
      }

      const pkg = cls.packageCode || t("classProperties.unknownPackage");
      const ns = uriNamespacePath(cls.iriLocal || cls.displayId?.split(":")[1] || "");
      if (!byPackage.has(pkg)) byPackage.set(pkg, new Map());
      const nsMap = byPackage.get(pkg)!;
      if (!nsMap.has(ns)) nsMap.set(ns, []);
      nsMap.get(ns)!.push({ cls, properties: rows });
    }

    const result: PackageGroup[] = [];
    const allPackageCodes = Array.from(byPackage.keys()).sort((a, b) => {
      const ai = packageOrder.indexOf(a);
      const bi = packageOrder.indexOf(b);
      if (ai >= 0 && bi >= 0) return ai - bi;
      if (ai >= 0) return -1;
      if (bi >= 0) return 1;
      return a.localeCompare(b);
    });

    for (const code of allPackageCodes) {
      const nsMap = byPackage.get(code)!;
      const namespaces: NamespaceGroup[] = Array.from(nsMap.entries())
        .sort(([a], [b]) => {
          if (!a) return -1;
          if (!b) return 1;
          return a.localeCompare(b);
        })
        .map(([path, classBlocks]) => ({
          path,
          classes: classBlocks.sort((a, b) =>
            pickLabel(a.cls.labels, i18n.language, a.cls.displayId || a.cls.id).localeCompare(
              pickLabel(b.cls.labels, i18n.language, b.cls.displayId || b.cls.id),
              undefined,
              { sensitivity: "base" },
            ),
          ),
        }));
      result.push({ code, packageLabels: packageLabels.get(code), namespaces });
    }
    return result;
  }, [classes, propsByClass, packages, query, i18n.language, t]);

  const totalClasses = grouped.reduce(
    (sum, pkg) => sum + pkg.namespaces.reduce((nsSum, ns) => nsSum + ns.classes.length, 0),
    0,
  );

  return (
    <div className="stack">
      <h1>{t("classProperties.title")}</h1>
      <p className="muted">{t("classProperties.subtitle")}</p>

      <div className="toolbar">
        <input
          style={{ flex: 1, minWidth: "12rem" }}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("classProperties.searchPlaceholder")}
        />
      </div>

      {error && <p className="error">{error}</p>}
      {loading && <p className="muted">{t("common.loading")}</p>}

      {!loading && !error && totalClasses === 0 && (
        <p className="muted">{query.trim() ? t("classProperties.noMatches") : t("classProperties.empty")}</p>
      )}

      {!loading &&
        grouped.map((pkg) => {
          const pkgLabel = pickLabel(pkg.packageLabels, i18n.language, pkg.code);
          return (
            <section key={pkg.code} className="stack">
              <h2>
                {pkgLabel} <span className="mono muted">({pkg.code})</span>
              </h2>
              {pkg.namespaces.map((ns) => (
                <div key={`${pkg.code}:${ns.path || "/"}`} className="stack compact">
                  <h3 className="muted">
                    {ns.path ? (
                      <>
                        {t("classProperties.namespace")}: <code>{ns.path}</code>
                      </>
                    ) : (
                      t("classProperties.rootNamespace")
                    )}
                  </h3>
                  {ns.classes.map(({ cls, properties: rows }) => (
                    <div key={cls.id} className="panel stack compact">
                      <div className="statement-group-header">
                        <EntityLink
                          id={cls.id}
                          displayId={cls.displayId}
                          labels={cls.labels}
                          lang={i18n.language}
                        />
                        <code className="muted">{cls.id}</code>
                      </div>
                      <table className="table">
                        <thead>
                          <tr>
                            <th>{t("classProperties.propertyLabel")}</th>
                            <th>{t("classProperties.valueType")}</th>
                            <th>{t("classProperties.description")}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {rows.map(({ property, required }) => (
                            <tr key={property.id}>
                              <td>
                                <EntityLink
                                  id={property.id}
                                  displayId={property.displayId}
                                  labels={property.labels}
                                  lang={i18n.language}
                                />
                                {required && (
                                  <span className="badge" style={{ marginLeft: "0.5rem" }}>
                                    {t("classProperties.required")}
                                  </span>
                                )}
                              </td>
                              <td>{formatValueType(property, i18n.language, classById)}</td>
                              <td>{pickLabel(property.descriptions, i18n.language, "—")}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ))}
                </div>
              ))}
            </section>
          );
        })}
    </div>
  );
}
