import React, { createContext, useContext, useEffect, useMemo, useState } from "react";
import { AuthSession, UiConfig, loadSession, loadUiConfig, saveSession } from "./api";

type AuthCtx = {
  cfg: UiConfig | null;
  session: AuthSession | null;
  setSession: (s: AuthSession | null) => void;
  ready: boolean;
};

const Ctx = createContext<AuthCtx>({ cfg: null, session: null, setSession: () => {}, ready: false });

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [cfg, setCfg] = useState<UiConfig | null>(null);
  const [session, setSessionState] = useState<AuthSession | null>(loadSession());
  const [ready, setReady] = useState(false);

  useEffect(() => {
    loadUiConfig()
      .then(setCfg)
      .catch(() => setCfg(null))
      .finally(() => setReady(true));
  }, []);

  const setSession = (s: AuthSession | null) => {
    saveSession(s);
    setSessionState(s);
  };

  const value = useMemo(() => ({ cfg, session, setSession, ready }), [cfg, session, ready]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth() {
  return useContext(Ctx);
}
