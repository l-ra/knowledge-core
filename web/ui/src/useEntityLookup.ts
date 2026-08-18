import { useEffect, useMemo, useState } from "react";
import { apiFetch } from "./api";

export type EntitySummary = {
  id: string;
  kind?: string;
  labels?: Record<string, string>;
  packageCode?: string;
};

const entityCache = new Map<string, EntitySummary>();

export function useEntityLookup(ids: string[]) {
  const normalized = useMemo(() => Array.from(new Set(ids.filter(Boolean))).sort(), [ids]);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    const missing = normalized.filter((id) => !entityCache.has(id));
    if (missing.length === 0) return;
    let cancelled = false;
    void Promise.all(
      missing.map(async (id) => {
        try {
          const entity = await apiFetch<EntitySummary>(`/v1/entities/${id}`);
          entityCache.set(id, entity);
        } catch {
          entityCache.set(id, { id });
        }
      }),
    ).then(() => {
      if (!cancelled) setVersion((v) => v + 1);
    });
    return () => {
      cancelled = true;
    };
  }, [normalized]);

  return useMemo(() => {
    void version;
    const out: Record<string, EntitySummary> = {};
    for (const id of normalized) {
      const entity = entityCache.get(id);
      if (entity) out[id] = entity;
    }
    return out;
  }, [normalized, version]);
}
