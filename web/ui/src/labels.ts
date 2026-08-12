/** Prefer UI language label, fall back to en, then any, then fallback id. */
export function pickLabel(
  labels: Record<string, string> | undefined,
  lang: string,
  fallback = "—",
): string {
  if (!labels) return fallback;
  const short = lang.split("-")[0]?.toLowerCase() || "en";
  if (labels[short]) return labels[short];
  if (labels.en) return labels.en;
  const first = Object.values(labels).find((v) => v?.trim());
  return first || fallback;
}
