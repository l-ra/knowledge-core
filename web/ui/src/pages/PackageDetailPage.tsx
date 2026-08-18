import { FormEvent, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";
import { pickLabel } from "../labels";
import { useChangeSetDraft } from "../changeset";
import { EntityLink, StatementLink } from "../links";

type PkgDep = { dependsOnCode: string; versionRange: string };
type Pkg = {
  code: string;
  lifecycle: string;
  labels?: Record<string, string>;
  iriBase?: string;
  dependencies?: PkgDep[];
  createdAt?: string;
  updatedAt?: string;
};
type OwnedObject = {
  objectType: string;
  publicId: string;
  displayId?: string;
  revisionNo: number;
  labels?: Record<string, string>;
};
type Release = {
  package: string;
  version: string;
  publishedAt: string;
  dependencies?: Array<{ dependencyCode: string; dependencyVersion: string }>;
  objects?: Array<{ objectType: string; publicId: string; revisionNo: number }>;
};

export function PackageDetailPage() {
  const { code = "" } = useParams();
  const { t, i18n } = useTranslation();
  const { lastCommittedId } = useChangeSetDraft();
  const [pkg, setPkg] = useState<Pkg | null>(null);
  const [objects, setObjects] = useState<OwnedObject[]>([]);
  const [releases, setReleases] = useState<Release[]>([]);
  const [version, setVersion] = useState("");
  const [iriBase, setIriBase] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [info, setInfo] = useState("");

  async function load() {
    setError("");
    try {
      const [p, objs, rels] = await Promise.all([
        apiFetch<Pkg>(`/v1/packages/${encodeURIComponent(code)}`),
        apiFetch<{ items: OwnedObject[] }>(`/v1/packages/${encodeURIComponent(code)}/objects`),
        apiFetch<{ items: Release[] }>(`/v1/packages/${encodeURIComponent(code)}/releases`),
      ]);
      setPkg(p);
      setIriBase(p.iriBase || "");
      setObjects(objs.items || []);
      setReleases(rels.items || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code]);

  async function saveIriBase(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setInfo("");
    try {
      await apiFetch(`/v1/packages/${encodeURIComponent(code)}`, {
        method: "PATCH",
        body: JSON.stringify({ iriBase }),
      });
      setInfo(t("packages.iriBaseSaved"));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setBusy(false);
    }
  }

  async function publish(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await apiFetch(`/v1/packages/${encodeURIComponent(code)}/releases`, {
        method: "POST",
        body: JSON.stringify({ version }),
      });
      setVersion("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    } finally {
      setBusy(false);
    }
  }

  async function exportBundle(ver: string) {
    setError("");
    try {
      const bundle = await apiFetch<unknown>(
        `/v1/packages/${encodeURIComponent(code)}/releases/${encodeURIComponent(ver)}/bundle`,
      );
      const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${code}-${ver}-bundle.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  if (!pkg && !error) return <p>{t("common.loading")}</p>;

  return (
    <div className="stack">
      <nav className="breadcrumb muted">
        <Link to="/model/packages">{t("nav.packages")}</Link>
        <span>
          {" / "}
          <strong>{code}</strong>
        </span>
      </nav>

      <header className="panel stack">
        <h1>{pickLabel(pkg?.labels, i18n.language, code)}</h1>
        <p className="muted">
          {code}
          {pkg?.lifecycle ? ` · ${pkg.lifecycle}` : ""}
        </p>
        {pkg?.iriBase && (
          <p className="muted mono">
            {t("packages.iriBase")}: {pkg.iriBase}
          </p>
        )}
        <form className="row" onSubmit={saveIriBase}>
          <label className="field" style={{ flex: 1 }}>
            {t("packages.iriBase")}
            <input
              value={iriBase}
              onChange={(e) => setIriBase(e.target.value)}
              placeholder="https://example.org/id/"
            />
            <span className="muted">{t("packages.iriBaseHint")}</span>
          </label>
          <button className="primary" type="submit" disabled={busy}>
            {t("packages.saveIriBase")}
          </button>
        </form>
        {info && <p className="muted">{info}</p>}
        {pkg?.dependencies && pkg.dependencies.length > 0 && (
          <div>
            <h3>{t("packages.dependencies")}</h3>
            <ul>
              {pkg.dependencies.map((d) => (
                <li key={`${d.dependsOnCode}:${d.versionRange}`}>
                  <Link to={`/model/packages/${encodeURIComponent(d.dependsOnCode)}`}>{d.dependsOnCode}</Link>
                  <span className="muted"> {d.versionRange}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
        {lastCommittedId && (
          <p className="muted">
            {t("changeset.lastCommitted")}: <code>{lastCommittedId}</code>
          </p>
        )}
      </header>

      {error && <p className="error">{error}</p>}

      <section className="stack">
        <h2>{t("packages.objects")}</h2>
        <table className="table">
          <thead>
            <tr>
              <th>{t("packages.objectType")}</th>
              <th>{t("entity.label")}</th>
              <th>Rev</th>
            </tr>
          </thead>
          <tbody>
            {objects.map((o) => (
              <tr key={`${o.objectType}:${o.publicId}`}>
                <td>{o.objectType}</td>
                <td>
                  {o.objectType === "statement" ? (
                    <StatementLink id={o.publicId} displayId={o.displayId} />
                  ) : (
                    <EntityLink id={o.publicId} displayId={o.displayId} labels={o.labels} lang={i18n.language} />
                  )}
                </td>
                <td>{o.revisionNo}</td>
              </tr>
            ))}
            {objects.length === 0 && (
              <tr>
                <td colSpan={3} className="muted">
                  {t("packages.noObjects")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      <section className="stack">
        <h2>{t("packages.releases")}</h2>
        <form className="panel row" onSubmit={publish}>
          <label className="field">
            {t("packages.version")}
            <input
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder="1.0.0"
              required
            />
          </label>
          <button className="primary" type="submit" disabled={busy}>
            {t("packages.publish")}
          </button>
        </form>
        <table className="table">
          <thead>
            <tr>
              <th>{t("packages.version")}</th>
              <th>{t("packages.publishedAt")}</th>
              <th>{t("packages.objects")}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {releases.map((r) => (
              <tr key={r.version}>
                <td>{r.version}</td>
                <td className="muted">{r.publishedAt}</td>
                <td>{r.objects?.length ?? 0}</td>
                <td>
                  <button type="button" onClick={() => void exportBundle(r.version)}>
                    {t("packages.exportBundle")}
                  </button>
                </td>
              </tr>
            ))}
            {releases.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  {t("packages.noReleases")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>
    </div>
  );
}
