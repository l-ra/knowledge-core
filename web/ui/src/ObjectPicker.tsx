import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "./api";
import { pickLabel } from "./labels";
import { ObjectLabel } from "./links";

type Hit = { id: string; labels?: Record<string, string>; kind?: string; displayId?: string };

/** Searchable picker for KC entities (optionally filtered by kind, e.g. class). */
export function ObjectPicker({
  value,
  onChange,
  kind,
  placeholder,
  required,
}: {
  value: string;
  onChange: (id: string) => void;
  kind?: "entity" | "property" | "class" | "";
  placeholder?: string;
  required?: boolean;
}) {
  const { t, i18n } = useTranslation();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<Hit[]>([]);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!q.trim()) {
      setHits([]);
      return;
    }
    const handle = window.setTimeout(() => {
      const sp = new URLSearchParams();
      sp.set("q", q);
      sp.set("limit", "15");
      if (kind) sp.set("kind", kind);
      void apiFetch<{ items: Hit[] }>(`/v1/entities?${sp}`)
        .then((res) => setHits(res.items || []))
        .catch(() => setHits([]));
    }, 200);
    return () => window.clearTimeout(handle);
  }, [q, kind]);

  return (
    <div className="entity-picker">
      <input
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        placeholder={placeholder || t("entity.searchEntity")}
        required={required}
      />
      <input
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        placeholder={t("entity.searchLabel")}
      />
      {open && hits.length > 0 && (
        <ul className="entity-picker-results">
          {hits.map((h) => (
            <li key={h.id}>
              <button
                type="button"
                onClick={() => {
                  onChange(h.id);
                  setQ(pickLabel(h.labels, i18n.language, h.displayId || h.id));
                  setOpen(false);
                }}
              >
                <ObjectLabel id={h.id} displayId={h.displayId} labels={h.labels} lang={i18n.language} />
                {h.kind ? ` · ${h.kind}` : ""}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Multi-select of object IDs with searchable add. */
export function ObjectMultiPicker({
  values,
  onChange,
  kind,
  placeholder,
}: {
  values: string[];
  onChange: (ids: string[]) => void;
  kind?: "entity" | "property" | "class" | "";
  placeholder?: string;
}) {
  const { t, i18n } = useTranslation();
  const [draft, setDraft] = useState("");
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<Hit[]>([]);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!q.trim()) {
      setHits([]);
      return;
    }
    const handle = window.setTimeout(() => {
      const sp = new URLSearchParams();
      sp.set("q", q);
      sp.set("limit", "15");
      if (kind) sp.set("kind", kind);
      void apiFetch<{ items: Hit[] }>(`/v1/entities?${sp}`)
        .then((res) => setHits(res.items || []))
        .catch(() => setHits([]));
    }, 200);
    return () => window.clearTimeout(handle);
  }, [q, kind]);

  function add(id: string) {
    const trimmed = id.trim();
    if (!trimmed) return;
    if (!values.includes(trimmed)) onChange([...values, trimmed]);
    setDraft("");
    setQ("");
    setHits([]);
    setOpen(false);
  }

  return (
    <div className="object-multi-picker stack">
      {values.length > 0 && (
        <ul className="chip-list">
          {values.map((id) => (
            <li key={id} className="chip">
              <ObjectLabel id={id} lang={i18n.language} />
              <button
                type="button"
                className="chip-remove"
                aria-label={t("common.remove")}
                onClick={() => onChange(values.filter((x) => x !== id))}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="entity-picker">
        <input
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            setQ(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add(draft);
            }
          }}
          placeholder={placeholder || t("entity.searchEntity")}
        />
        <input
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setOpen(true);
          }}
          placeholder={t("entity.searchLabel")}
        />
        {open && hits.length > 0 && (
          <ul className="entity-picker-results">
            {hits.map((h) => (
              <li key={h.id}>
                <button type="button" onClick={() => add(h.id)}>
                  <ObjectLabel id={h.id} displayId={h.displayId} labels={h.labels} lang={i18n.language} />
                  {h.kind ? ` · ${h.kind}` : ""}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
