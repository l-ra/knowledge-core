import { Link } from "react-router-dom";
import { pickLabel } from "./labels";

type Labels = Record<string, string> | undefined;

function IdSuffix({ id }: { id: string }) {
  return <span className="object-id">{id}</span>;
}

export function ObjectLabel({
  id,
  labels,
  lang,
  fallback,
}: {
  id: string;
  labels?: Labels;
  lang: string;
  fallback?: string;
}) {
  return (
    <span className="object-label">
      <span>{pickLabel(labels, lang, fallback || id)}</span>
      <IdSuffix id={id} />
    </span>
  );
}

export function EntityLink({
  id,
  labels,
  lang,
  breadcrumb,
}: {
  id: string;
  labels?: Labels;
  lang: string;
  breadcrumb?: string[];
}) {
  return (
    <Link to={`/entities/${id}`} state={{ breadcrumb: breadcrumb || [] }}>
      <ObjectLabel id={id} labels={labels} lang={lang} />
    </Link>
  );
}

export function StatementLink({ id, text }: { id: string; text?: string }) {
  return (
    <Link to={`/statements/${id}`}>
      <span className="object-label">
        <span>{text || "Statement"}</span>
        <IdSuffix id={id} />
      </span>
    </Link>
  );
}

export function ReferenceLink({ id, text }: { id: string; text?: string }) {
  return (
    <Link to={`/references/${id}`}>
      <span className="object-label">
        <span>{text || "Reference"}</span>
        <IdSuffix id={id} />
      </span>
    </Link>
  );
}
