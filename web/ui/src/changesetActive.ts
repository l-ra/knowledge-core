const ACTIVE_CS_KEY = "kc.activeChangeSet";

export type StoredActiveChangeSet = {
  id: string;
  status: string;
  comment?: string;
  openedAt?: string;
  claimCount?: number;
};

export function loadStoredActiveChangeSet(): StoredActiveChangeSet | null {
  try {
    const raw = sessionStorage.getItem(ACTIVE_CS_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as StoredActiveChangeSet;
  } catch {
    return null;
  }
}

export function storeActiveChangeSet(cs: StoredActiveChangeSet | null) {
  if (!cs) sessionStorage.removeItem(ACTIVE_CS_KEY);
  else sessionStorage.setItem(ACTIVE_CS_KEY, JSON.stringify(cs));
}

export function getActiveChangeSetId(): string {
  return loadStoredActiveChangeSet()?.id || "";
}
