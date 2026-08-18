import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { apiFetch } from "./api";
import { pickLabel } from "./labels";
import { ObjectLabel } from "./links";

export type ApiValue = Record<string, unknown>;

type EntityHit = { id: string; labels?: Record<string, string>; kind?: string };

const DATATYPES_NEEDING_ENTITY = new Set(["EntityReference"]);

function emptyValue(datatype: string): ApiValue {
  switch (datatype) {
    case "EntityReference":
      return { type: "EntityReference", entityId: "" };
    case "Boolean":
      return { type: "Boolean", bool: false };
    case "Integer":
      return { type: "Integer", int64: 0 };
    case "Decimal":
      return { type: "Decimal", decimal: "0" };
    case "Date":
      return { type: "Date", date: "" };
    case "DateTime":
      return { type: "DateTime", dateTime: "" };
    case "URI":
      return { type: "URI", uri: "" };
    case "LocalizedString":
      return { type: "LocalizedString", langMap: { en: "" } };
    case "ExternalIdentifier":
      return { type: "ExternalIdentifier", scheme: "", value: "" };
    case "Quantity":
      return { type: "Quantity", quantityValue: "", unitEntityId: "" };
    case "Interval":
      return { type: "Interval", from: null, to: null };
    default:
      return { type: "String", string: "" };
  }
}

export function valueFromApi(datatype: string, v?: ApiValue | null): ApiValue {
  if (!v || !v.type) return emptyValue(datatype === "Any" ? "String" : datatype);
  if (datatype === "Any") return { ...v };
  return { ...emptyValue(datatype), ...v, type: datatype };
}

export function displayValueText(v: ApiValue | undefined, lang: string): string {
  if (!v) return "—";
  if (v.string != null) return String(v.string);
  if (v.entityId != null) return String(v.entityId);
  if (v.uri != null) return String(v.uri);
  if (v.bool != null) return String(v.bool);
  if (v.int64 != null) return String(v.int64);
  if (v.decimal != null) return String(v.decimal);
  if (v.date != null) return String(v.date);
  if (v.dateTime != null) return String(v.dateTime);
  if (v.langMap && typeof v.langMap === "object") {
    return pickLabel(v.langMap as Record<string, string>, lang);
  }
  if (v.scheme != null || v.value != null) return `${v.scheme || ""}:${v.value || ""}`;
  if (v.quantityValue != null) return `${v.quantityValue}${v.unitEntityId ? ` ${v.unitEntityId}` : ""}`;
  return JSON.stringify(v);
}

function EntityPicker({
  value,
  onChange,
  required,
}: {
  value: string;
  onChange: (id: string) => void;
  required?: boolean;
}) {
  const { t, i18n } = useTranslation();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<EntityHit[]>([]);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!q.trim()) {
      setHits([]);
      return;
    }
    const handle = window.setTimeout(() => {
      void apiFetch<{ items: EntityHit[] }>(`/v1/entities?q=${encodeURIComponent(q)}&limit=15`)
        .then((res) => setHits(res.items || []))
        .catch(() => setHits([]));
    }, 200);
    return () => window.clearTimeout(handle);
  }, [q]);

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
        placeholder={t("entity.searchEntity")}
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
                  setQ(pickLabel(h.labels, i18n.language, h.id));
                  setOpen(false);
                }}
              >
                <ObjectLabel id={h.id} labels={h.labels} lang={i18n.language} />
                {h.kind ? ` · ${h.kind}` : ""}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function ValueEditor({
  datatype,
  value,
  onChange,
}: {
  datatype: string;
  value: ApiValue;
  onChange: (v: ApiValue) => void;
}) {
  const { t } = useTranslation();
  const concreteTypes = [
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
  ];

  if (datatype === "Any") {
    const innerType = typeof value?.type === "string" && value.type !== "Any" ? String(value.type) : "String";
    return (
      <div className="stack">
        <label className="field">
          {t("properties.valueType")}
          <select
            value={innerType}
            onChange={(e) => onChange(emptyValue(e.target.value))}
          >
            {concreteTypes.map((d) => (
              <option key={d} value={d}>
                {d}
              </option>
            ))}
          </select>
        </label>
        <ValueEditor datatype={innerType} value={value} onChange={onChange} />
      </div>
    );
  }

  const v = value?.type ? value : emptyValue(datatype);

  switch (datatype) {
    case "Boolean":
      return (
        <select
          value={v.bool === true ? "true" : "false"}
          onChange={(e) => onChange({ type: "Boolean", bool: e.target.value === "true" })}
        >
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      );
    case "Integer":
      return (
        <input
          type="number"
          step={1}
          value={v.int64 != null ? Number(v.int64) : 0}
          onChange={(e) => onChange({ type: "Integer", int64: Number(e.target.value) })}
          required
        />
      );
    case "Decimal":
      return (
        <input
          value={String(v.decimal ?? "")}
          onChange={(e) => onChange({ type: "Decimal", decimal: e.target.value })}
          required
        />
      );
    case "Date":
      return (
        <input
          type="date"
          value={String(v.date ?? "")}
          onChange={(e) => onChange({ type: "Date", date: e.target.value })}
          required
        />
      );
    case "DateTime":
      return (
        <input
          type="datetime-local"
          value={String(v.dateTime ?? "").slice(0, 16)}
          onChange={(e) =>
            onChange({
              type: "DateTime",
              dateTime: e.target.value ? new Date(e.target.value).toISOString() : "",
            })
          }
          required
        />
      );
    case "URI":
      return (
        <input
          type="url"
          value={String(v.uri ?? "")}
          onChange={(e) => onChange({ type: "URI", uri: e.target.value })}
          required
        />
      );
    case "EntityReference":
      return (
        <EntityPicker
          value={String(v.entityId ?? "")}
          onChange={(id) => onChange({ type: "EntityReference", entityId: id })}
          required
        />
      );
    case "LocalizedString": {
      const map = (v.langMap as Record<string, string>) || { en: "" };
      const entries = Object.entries(map);
      return (
        <div className="stack">
          {entries.map(([lang, text]) => (
            <div className="row" key={lang}>
              <input
                style={{ maxWidth: "4rem" }}
                value={lang}
                onChange={(e) => {
                  const next = { ...map };
                  delete next[lang];
                  next[e.target.value || "en"] = text;
                  onChange({ type: "LocalizedString", langMap: next });
                }}
              />
              <input
                style={{ flex: 1 }}
                value={text}
                onChange={(e) => onChange({ type: "LocalizedString", langMap: { ...map, [lang]: e.target.value } })}
                required
              />
            </div>
          ))}
          <button
            type="button"
            onClick={() => onChange({ type: "LocalizedString", langMap: { ...map, [`l${entries.length}`]: "" } })}
          >
            {t("entity.addLang")}
          </button>
        </div>
      );
    }
    case "ExternalIdentifier":
      return (
        <div className="row">
          <input
            placeholder={t("entity.scheme")}
            value={String(v.scheme ?? "")}
            onChange={(e) => onChange({ type: "ExternalIdentifier", scheme: e.target.value, value: v.value })}
            required
          />
          <input
            placeholder={t("entity.extId")}
            value={String(v.value ?? "")}
            onChange={(e) => onChange({ type: "ExternalIdentifier", scheme: v.scheme, value: e.target.value })}
            required
          />
        </div>
      );
    case "Quantity":
      return (
        <div className="stack">
          <input
            placeholder={t("entity.quantityValue")}
            value={String(v.quantityValue ?? "")}
            onChange={(e) =>
              onChange({ type: "Quantity", quantityValue: e.target.value, unitEntityId: v.unitEntityId })
            }
            required
          />
          <EntityPicker
            value={String(v.unitEntityId ?? "")}
            onChange={(id) => onChange({ type: "Quantity", quantityValue: v.quantityValue, unitEntityId: id })}
          />
        </div>
      );
    case "Interval":
      return (
        <div className="row">
          <label className="field">
            {t("entity.intervalFrom")}
            <textarea
              rows={2}
              value={v.from ? JSON.stringify(v.from) : ""}
              onChange={(e) => {
                let from: unknown = null;
                try {
                  from = e.target.value ? JSON.parse(e.target.value) : null;
                } catch {
                  from = e.target.value;
                }
                onChange({ type: "Interval", from, to: v.to });
              }}
            />
          </label>
          <label className="field">
            {t("entity.intervalTo")}
            <textarea
              rows={2}
              value={v.to ? JSON.stringify(v.to) : ""}
              onChange={(e) => {
                let to: unknown = null;
                try {
                  to = e.target.value ? JSON.parse(e.target.value) : null;
                } catch {
                  to = e.target.value;
                }
                onChange({ type: "Interval", from: v.from, to });
              }}
            />
          </label>
        </div>
      );
    default:
      return (
        <input
          value={String(v.string ?? "")}
          onChange={(e) => onChange({ type: "String", string: e.target.value })}
          required
        />
      );
  }
}

export { emptyValue, DATATYPES_NEEDING_ENTITY };
