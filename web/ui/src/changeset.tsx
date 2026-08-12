import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { apiFetch } from "./api";
import { usePackage } from "./package";

export type ChangeOperation = {
  op: string;
  clientKey?: string;
  packageCode?: string;
  entity?: string;
  statement?: string;
  subject?: string;
  property?: string;
  datatype?: string;
  constraints?: Record<string, unknown>;
  subClassOf?: string;
  value?: unknown;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
  expectedRevision?: number;
  qualifiers?: unknown[];
  referenceIds?: string[];
  validFrom?: string;
  validTo?: string;
};

export type ChangeSetDraft = {
  open: boolean;
  title?: string;
  packageCode?: string;
  operations: ChangeOperation[];
  updatedAt?: string;
  createdAt?: string;
};

type ChangeSetCtx = {
  draft: ChangeSetDraft;
  ready: boolean;
  isOpen: boolean;
  lastCommittedId: string;
  openDraft: (opts?: { title?: string; packageCode?: string }) => Promise<void>;
  closeDraft: () => Promise<void>;
  saveDraft: () => Promise<void>;
  commitDraft: () => Promise<string>;
  queueOp: (op: ChangeOperation) => void;
  runWrite: <T>(immediateFn: () => Promise<T>, draftOp: ChangeOperation) => Promise<T | void>;
};

const emptyDraft = (): ChangeSetDraft => ({ open: false, operations: [] });

const Ctx = createContext<ChangeSetCtx>({
  draft: emptyDraft(),
  ready: false,
  isOpen: false,
  lastCommittedId: "",
  openDraft: async () => {},
  closeDraft: async () => {},
  saveDraft: async () => {},
  commitDraft: async () => "",
  queueOp: () => {},
  runWrite: async (fn) => fn(),
});

let clientKeySeq = 0;
export function nextClientKey(prefix = "k"): string {
  clientKeySeq += 1;
  return `${prefix}-${Date.now()}-${clientKeySeq}`;
}

export function ChangeSetDraftProvider({ children }: { children: React.ReactNode }) {
  const { packageCode } = usePackage();
  const [draft, setDraft] = useState<ChangeSetDraft>(emptyDraft());
  const [ready, setReady] = useState(false);
  const [lastCommittedId, setLastCommittedId] = useState("");
  const draftRef = useRef(draft);
  draftRef.current = draft;

  const reload = useCallback(async () => {
    try {
      const d = await apiFetch<ChangeSetDraft>("/v1/me/changeset-draft");
      setDraft({
        open: !!d.open,
        title: d.title,
        packageCode: d.packageCode,
        operations: d.operations || [],
        updatedAt: d.updatedAt,
        createdAt: d.createdAt,
      });
    } catch {
      setDraft(emptyDraft());
    } finally {
      setReady(true);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const putDraft = useCallback(async (next: ChangeSetDraft) => {
    const saved = await apiFetch<ChangeSetDraft>("/v1/me/changeset-draft", {
      method: "PUT",
      body: JSON.stringify({
        open: next.open,
        title: next.title || undefined,
        packageCode: next.packageCode || undefined,
        operations: next.operations || [],
      }),
    });
    const normalized: ChangeSetDraft = {
      open: !!saved.open,
      title: saved.title,
      packageCode: saved.packageCode,
      operations: saved.operations || [],
      updatedAt: saved.updatedAt,
      createdAt: saved.createdAt,
    };
    setDraft(normalized);
    return normalized;
  }, []);

  const openDraft = useCallback(
    async (opts?: { title?: string; packageCode?: string }) => {
      await putDraft({
        open: true,
        title: opts?.title,
        packageCode: opts?.packageCode || packageCode || undefined,
        operations: draftRef.current.open ? draftRef.current.operations : [],
      });
    },
    [packageCode, putDraft],
  );

  const closeDraft = useCallback(async () => {
    await apiFetch("/v1/me/changeset-draft", { method: "DELETE" });
    setDraft(emptyDraft());
  }, []);

  const saveDraft = useCallback(async () => {
    await putDraft(draftRef.current);
  }, [putDraft]);

  const commitDraft = useCallback(async () => {
    const res = await apiFetch<{ changeSet?: string; id?: string }>("/v1/me/changeset-draft/commit", {
      method: "POST",
      body: "{}",
    });
    const id = res.changeSet || res.id || "";
    setLastCommittedId(id);
    setDraft(emptyDraft());
    return id;
  }, []);

  const queueOp = useCallback(
    (op: ChangeOperation) => {
      const withKey: ChangeOperation = {
        ...op,
        clientKey: op.clientKey || nextClientKey("op"),
      };
      const next: ChangeSetDraft = {
        ...draftRef.current,
        open: true,
        packageCode: draftRef.current.packageCode || packageCode || undefined,
        operations: [...(draftRef.current.operations || []), withKey],
      };
      setDraft(next);
      void putDraft(next).catch(() => {
        // keep optimistic state; caller may surface errors on next action
      });
    },
    [packageCode, putDraft],
  );

  const runWrite = useCallback(
    async <T,>(immediateFn: () => Promise<T>, draftOp: ChangeOperation): Promise<T | void> => {
      if (draftRef.current.open) {
        queueOp(draftOp);
        return;
      }
      return immediateFn();
    },
    [queueOp],
  );

  const value = useMemo(
    () => ({
      draft,
      ready,
      isOpen: !!draft.open,
      lastCommittedId,
      openDraft,
      closeDraft,
      saveDraft,
      commitDraft,
      queueOp,
      runWrite,
    }),
    [draft, ready, lastCommittedId, openDraft, closeDraft, saveDraft, commitDraft, queueOp, runWrite],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useChangeSetDraft() {
  return useContext(Ctx);
}
