import React, { createContext, useContext, useEffect, useMemo, useState } from "react";
import { apiFetch } from "./api";

type Pkg = { code: string; labels?: Record<string, string> };

type PackageCtx = {
  packages: Pkg[];
  packageCode: string;
  setPackageCode: (code: string) => void;
  ready: boolean;
  reload: () => Promise<void>;
};

const Ctx = createContext<PackageCtx>({
  packages: [],
  packageCode: "",
  setPackageCode: () => {},
  ready: false,
  reload: async () => {},
});

const STORAGE_KEY = "kc.packageCode";

export function PackageProvider({ children }: { children: React.ReactNode }) {
  const [packages, setPackages] = useState<Pkg[]>([]);
  const [packageCode, setPackageCodeState] = useState(() => localStorage.getItem(STORAGE_KEY) || "");
  const [ready, setReady] = useState(false);

  async function reload() {
    try {
      const res = await apiFetch<{ items: Pkg[] }>("/v1/packages");
      const items = res.items || [];
      setPackages(items);
      setPackageCodeState((prev) => {
        if (prev && items.some((p) => p.code === prev)) return prev;
        const next = items[0]?.code || "";
        if (next) localStorage.setItem(STORAGE_KEY, next);
        else localStorage.removeItem(STORAGE_KEY);
        return next;
      });
    } catch {
      setPackages([]);
    } finally {
      setReady(true);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  const setPackageCode = (code: string) => {
    setPackageCodeState(code);
    if (code) localStorage.setItem(STORAGE_KEY, code);
    else localStorage.removeItem(STORAGE_KEY);
  };

  const value = useMemo(
    () => ({ packages, packageCode, setPackageCode, ready, reload }),
    [packages, packageCode, ready],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function usePackage() {
  return useContext(Ctx);
}
