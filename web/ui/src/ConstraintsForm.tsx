import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ObjectMultiPicker } from "./ObjectPicker";

export type ConstraintsFormState = {
  domainClasses: string[];
  rangeClasses: string[];
  minCount: string;
  maxCount: string;
  severity: "" | "error" | "warning" | "info";
};

export const emptyConstraintsForm = (): ConstraintsFormState => ({
  domainClasses: [],
  rangeClasses: [],
  minCount: "",
  maxCount: "",
  severity: "",
});

/** Build API constraints object; omits empty fields. */
export function constraintsFromForm(form: ConstraintsFormState): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (form.domainClasses.length) out.domainClasses = form.domainClasses;
  if (form.rangeClasses.length) out.rangeClasses = form.rangeClasses;
  if (form.minCount.trim() !== "") {
    const n = Number(form.minCount);
    if (Number.isFinite(n)) out.minCount = Math.trunc(n);
  }
  if (form.maxCount.trim() !== "") {
    const n = Number(form.maxCount);
    if (Number.isFinite(n)) out.maxCount = Math.trunc(n);
  }
  if (form.severity) out.severity = form.severity;
  return out;
}

export function ConstraintsForm({
  value,
  onChange,
}: {
  value: ConstraintsFormState;
  onChange: (next: ConstraintsFormState) => void;
}) {
  const { t } = useTranslation();
  const jsonPreview = useMemo(() => JSON.stringify(constraintsFromForm(value), null, 2), [value]);

  return (
    <div className="stack constraints-form">
      <label className="field">
        {t("properties.domainClasses")}
        <ObjectMultiPicker
          values={value.domainClasses}
          onChange={(domainClasses) => onChange({ ...value, domainClasses })}
          kind="class"
          placeholder={t("properties.pickClass")}
        />
      </label>
      <label className="field">
        {t("properties.rangeClasses")}
        <ObjectMultiPicker
          values={value.rangeClasses}
          onChange={(rangeClasses) => onChange({ ...value, rangeClasses })}
          kind="class"
          placeholder={t("properties.pickClass")}
        />
      </label>
      <div className="row" style={{ gap: "1rem", flexWrap: "wrap" }}>
        <label className="field">
          {t("properties.minCount")}
          <input
            type="number"
            min={0}
            value={value.minCount}
            onChange={(e) => onChange({ ...value, minCount: e.target.value })}
            placeholder="—"
          />
        </label>
        <label className="field">
          {t("properties.maxCount")}
          <input
            type="number"
            min={0}
            value={value.maxCount}
            onChange={(e) => onChange({ ...value, maxCount: e.target.value })}
            placeholder="—"
          />
        </label>
        <label className="field">
          {t("properties.severity")}
          <select
            value={value.severity}
            onChange={(e) =>
              onChange({ ...value, severity: e.target.value as ConstraintsFormState["severity"] })
            }
          >
            <option value="">{t("properties.severityDefault")}</option>
            <option value="error">{t("properties.severityError")}</option>
            <option value="warning">{t("properties.severityWarning")}</option>
            <option value="info">{t("properties.severityInfo")}</option>
          </select>
        </label>
      </div>
      <label className="field">
        {t("properties.constraintsRaw")}
        <textarea className="mono" rows={6} value={jsonPreview} readOnly />
      </label>
    </div>
  );
}
