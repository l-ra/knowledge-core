import { Link } from "react-router-dom";
import { pickLabel } from "./labels";

type Labels = Record<string, string> | undefined;

function compactId(id: string) {
  const kcUrn = id.match(/^urn:kc:([^:]+):(.+)$/);
  if (kcUrn) {
    return `${kcUrn[1]}:${kcUrn[2]}`;
  }
  if (/^[a-z]+:\/\//i.test(id)) {
    const hash = id.lastIndexOf("#");
    const slash = id.lastIndexOf("/");
    const cut = Math.max(hash, slash);
    if (cut >= 0 && cut + 1 < id.length) return id.slice(cut + 1);
  }
  if (id.startsWith("urn:")) {
    const parts = id.split(":");
    return parts[parts.length - 1] || id;
  }
  return id;
}

function IdSuffix({ id }: { id: string }) {
  return <span className="object-id">{compactId(id)}</span>;
}

export function ObjectLabel({
  id,
  displayId,
  labels,
  lang,
  fallback,
}: {
  id: string;
  displayId?: string;
  labels?: Labels;
  lang: string;
  fallback?: string;
}) {
  return (
    <span className="object-label">
      <span>{pickLabel(labels, lang, fallback || id)}</span>
      <IdSuffix id={displayId || id} />
    </span>
  );
}

export function EntityLink({
  id,
  displayId,
  labels,
  lang,
  breadcrumb,
}: {
  id: string;
  displayId?: string;
  labels?: Labels;
  lang: string;
  breadcrumb?: string[];
}) {
  const state = breadcrumb ? { breadcrumb } : undefined;
  return (
    <Link to={`/entities/${encodeURIComponent(id)}`} state={state}>
      <ObjectLabel id={id} displayId={displayId} labels={labels} lang={lang} />
    </Link>
  );
}

export function StatementLink({ id, displayId, text }: { id: string; displayId?: string; text?: string }) {
  return (
    <Link to={`/statements/${encodeURIComponent(id)}`}>
      <span className="object-label">
        <span>{text || "Statement"}</span>
        <IdSuffix id={displayId || id} />
      </span>
    </Link>
  );
}

export function ReferenceLink({ id, displayId, text }: { id: string; displayId?: string; text?: string }) {
  return (
    <Link to={`/references/${encodeURIComponent(id)}`}>
      <span className="object-label">
        <span>{text || "Reference"}</span>
        <IdSuffix id={displayId || id} />
      </span>
    </Link>
  );
}
