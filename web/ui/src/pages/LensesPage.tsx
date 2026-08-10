import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../api";

type Lens = { code: string; labels?: Record<string, string>; version: number; document: unknown };

export function LensesPage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<Lens[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    apiFetch<{ items: Lens[] }>("/v1/lenses")
      .then((r) => setItems(r.items || []))
      .catch((err) => setError(err instanceof Error ? err.message : t("common.error")));
  }, [t]);

  return (
    <div className="stack">
      <h1>{t("lenses.title")}</h1>
      {error && <p className="error">{error}</p>}
      {items.map((l) => (
        <div className="panel" key={l.code}>
          <h2>
            {l.code} <span className="muted">v{l.version}</span>
          </h2>
          <p>{l.labels?.en}</p>
          <pre style={{ overflow: "auto" }}>{JSON.stringify(l.document, null, 2)}</pre>
        </div>
      ))}
    </div>
  );
}
