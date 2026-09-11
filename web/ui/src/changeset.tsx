import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { apiFetch } from "./api";
import {
  getActiveChangeSetId,
  loadStoredActiveChangeSet,
  storeActiveChangeSet,
  type StoredActiveChangeSet,
} from "./changesetActive";

export type ActiveChangeSet = StoredActiveChangeSet;

type ChangeSetMeta = {
  id: string;
  status: string;
  comment?: string;
  operationType?: string;
  openedAt?: string;
  claims?: unknown[];
};

type UpdateChangeSetOpts = {
  comment?: string;
  operationType?: string;
};

type ChangeSetCtx = {
  ready: boolean;
  isOpen: boolean;
  active: ActiveChangeSet | null;
  lastCommittedId: string;
  openChangeSet: (opts?: { comment?: string; operationType?: string }) => Promise<void>;
  /** Activate an existing open ChangeSet so subsequent writes append to it. */
  enterChangeSet: (id: string) => Promise<void>;
  updateChangeSet: (id: string, opts: UpdateChangeSetOpts) => Promise<ChangeSetMeta>;
  /** Leave active ChangeSet in UI only; server CS stays open. */
  leaveChangeSet: () => void;
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
  enterChangeSet: async () => {},
  updateChangeSet: async () => ({ id: "", status: "" }),
  leaveChangeSet: () => {},
  cancelChangeSet: async () => {},
  commitChangeSet: async () => "",
  runWrite: async (fn) => fn(),
  readHeaders: () => ({}),
});

type WriteEnvelope<T> = { data?: T; changeSet?: { id?: string } };

function toActive(cs: ChangeSetMeta, claimCount?: number): ActiveChangeSet {
  return {
    id: cs.id,
    status: cs.status || "open",
    comment: cs.comment,
    openedAt: cs.openedAt,
    claimCount: claimCount ?? (Array.isArray(cs.claims) ? cs.claims.length : 0),
  };
}

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
    void apiFetch<ChangeSetMeta>(`/v1/changesets/${encodeURIComponent(stored.id)}`)
      .then((cs) => {
        if (cs.status !== "open") {
          storeActiveChangeSet(null);
          setActive(null);
          return;
        }
        const next = toActive(cs, Array.isArray(cs.claims) ? cs.claims.length : stored.claimCount);
        storeActiveChangeSet(next);
        setActive(next);
      })
      .catch(() => {
        storeActiveChangeSet(null);
        setActive(null);
      })
      .finally(() => setReady(true));
  }, []);

  const openChangeSet = useCallback(async (opts?: { comment?: string; operationType?: string }) => {
    const res = await apiFetch<WriteEnvelope<ChangeSetMeta>>("/v1/changesets/open", {
      method: "POST",
      body: JSON.stringify({
        comment: opts?.comment || undefined,
        operationType: opts?.operationType || undefined,
      }),
    });
    const cs = res.data || (res as unknown as ChangeSetMeta);
    const next = toActive(cs, 0);
    storeActiveChangeSet(next);
    setActive(next);
  }, []);

  const enterChangeSet = useCallback(async (id: string) => {
    const cs = await apiFetch<ChangeSetMeta>(`/v1/changesets/${encodeURIComponent(id)}`);
    if (cs.status !== "open") {
      throw new Error("ChangeSet is not open");
    }
    const next = toActive(cs);
    storeActiveChangeSet(next);
    setActive(next);
  }, []);

  const updateChangeSet = useCallback(async (id: string, opts: UpdateChangeSetOpts) => {
    const body: Record<string, string> = {};
    if (opts.comment !== undefined) body.comment = opts.comment;
    if (opts.operationType !== undefined) body.operationType = opts.operationType;
    const res = await apiFetch<WriteEnvelope<ChangeSetMeta>>(
      `/v1/changesets/${encodeURIComponent(id)}`,
      { method: "PATCH", body: JSON.stringify(body) },
    );
    const cs = res.data || (res as unknown as ChangeSetMeta);
    if (getActiveChangeSetId() === id) {
      const next = toActive(
        { ...cs, id: cs.id || id, status: cs.status || "open" },
        active?.claimCount,
      );
      storeActiveChangeSet(next);
      setActive(next);
    }
    return cs;
  }, [active?.claimCount]);

  const leaveChangeSet = useCallback(() => {
    storeActiveChangeSet(null);
    setActive(null);
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
      enterChangeSet,
      updateChangeSet,
      leaveChangeSet,
      cancelChangeSet,
      commitChangeSet,
      runWrite,
      readHeaders,
    }),
    [
      ready,
      active,
      lastCommittedId,
      openChangeSet,
      enterChangeSet,
      updateChangeSet,
      leaveChangeSet,
      cancelChangeSet,
      commitChangeSet,
      runWrite,
      readHeaders,
    ],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useChangeSetDraft() {
  return useContext(Ctx);
}

export function nextClientKey(prefix = "k"): string {
  return `${prefix}-${Date.now()}`;
}
