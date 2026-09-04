import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { apiFetch } from "./api";
import {
  getActiveChangeSetId,
  loadStoredActiveChangeSet,
  storeActiveChangeSet,
  type StoredActiveChangeSet,
} from "./changesetActive";

export type ActiveChangeSet = StoredActiveChangeSet;

type ChangeSetCtx = {
  ready: boolean;
  isOpen: boolean;
  active: ActiveChangeSet | null;
  lastCommittedId: string;
  openChangeSet: (opts?: { comment?: string }) => Promise<void>;
  cancelChangeSet: () => Promise<void>;
  commitChangeSet: () => Promise<string>;
  /** Always executes the write; when a CS is open, apiFetch adds X-Knowledge-Changeset. */
  runWrite: <T>(immediateFn: () => Promise<T>, _ignored?: unknown) => Promise<T>;
  readHeaders: () => Record<string, string>;
};

const Ctx = createContext<ChangeSetCtx>({
  ready: false,
  isOpen: false,
  active: null,
  lastCommittedId: "",
  openChangeSet: async () => {},
  cancelChangeSet: async () => {},
  commitChangeSet: async () => "",
  runWrite: async (fn) => fn(),
  readHeaders: () => ({}),
});

type WriteEnvelope<T> = { data?: T; changeSet?: { id?: string } };

export function ChangeSetDraftProvider({ children }: { children: React.ReactNode }) {
  const [active, setActive] = useState<ActiveChangeSet | null>(null);
  const [ready, setReady] = useState(false);
  const [lastCommittedId, setLastCommittedId] = useState("");

  useEffect(() => {
    const stored = loadStoredActiveChangeSet();
    if (!stored?.id) {
      setReady(true);
      return;
    }
    void apiFetch<{ id: string; status: string; comment?: string; openedAt?: string; claims?: unknown[] }>(
      `/v1/changesets/${encodeURIComponent(stored.id)}`,
    )
      .then((cs) => {
        if (cs.status !== "open") {
          storeActiveChangeSet(null);
          setActive(null);
          return;
        }
        const next: ActiveChangeSet = {
          id: cs.id,
          status: cs.status,
          comment: cs.comment,
          openedAt: cs.openedAt,
          claimCount: Array.isArray(cs.claims) ? cs.claims.length : stored.claimCount,
        };
        storeActiveChangeSet(next);
        setActive(next);
      })
      .catch(() => {
        storeActiveChangeSet(null);
        setActive(null);
      })
      .finally(() => setReady(true));
  }, []);

  const openChangeSet = useCallback(async (opts?: { comment?: string }) => {
    const res = await apiFetch<WriteEnvelope<{ id: string; status: string; comment?: string; openedAt?: string }>>(
      "/v1/changesets/open",
      {
        method: "POST",
        body: JSON.stringify({ comment: opts?.comment || undefined }),
      },
    );
    const cs = res.data || (res as unknown as { id: string; status: string; comment?: string; openedAt?: string });
    const next: ActiveChangeSet = {
      id: cs.id,
      status: cs.status || "open",
      comment: cs.comment,
      openedAt: cs.openedAt,
      claimCount: 0,
    };
    storeActiveChangeSet(next);
    setActive(next);
  }, []);

  const cancelChangeSet = useCallback(async () => {
    const id = getActiveChangeSetId();
    if (!id) {
      setActive(null);
      return;
    }
    await apiFetch(`/v1/changesets/${encodeURIComponent(id)}/cancel`, { method: "POST", body: "{}" });
    storeActiveChangeSet(null);
    setActive(null);
  }, []);

  const commitChangeSet = useCallback(async () => {
    const id = getActiveChangeSetId();
    if (!id) return "";
    const res = await apiFetch<WriteEnvelope<{ id: string }>>(
      `/v1/changesets/${encodeURIComponent(id)}/commit`,
      { method: "POST", body: "{}" },
    );
    const committed = res.data?.id || res.changeSet?.id || id;
    setLastCommittedId(committed);
    storeActiveChangeSet(null);
    setActive(null);
    return committed;
  }, []);

  const runWrite = useCallback(async <T,>(immediateFn: () => Promise<T>, _ignored?: unknown): Promise<T> => {
    return immediateFn();
  }, []);

  const readHeaders = useCallback((): Record<string, string> => {
    const id = getActiveChangeSetId();
    return id ? { "X-Knowledge-Changesets": id } : {};
  }, []);

  const value = useMemo(
    () => ({
      ready,
      isOpen: !!active?.id,
      active,
      lastCommittedId,
      openChangeSet,
      cancelChangeSet,
      commitChangeSet,
      runWrite,
      readHeaders,
    }),
    [ready, active, lastCommittedId, openChangeSet, cancelChangeSet, commitChangeSet, runWrite, readHeaders],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useChangeSetDraft() {
  return useContext(Ctx);
}

export function nextClientKey(prefix = "k"): string {
  return `${prefix}-${Date.now()}`;
}
