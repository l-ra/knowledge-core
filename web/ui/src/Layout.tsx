import { NavLink, Outlet } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useState } from "react";
import { useAuth } from "./auth";
import { usePackage } from "./package";
import { useChangeSetDraft } from "./changeset";
import i18n from "./i18n";

export function Layout() {
  const { t } = useTranslation();
  const { session, setSession } = useAuth();
  const { packages, packageCode, setPackageCode } = usePackage();
  const { draft, isOpen, openDraft, closeDraft, commitDraft, lastCommittedId } = useChangeSetDraft();
  const [opsOpen, setOpsOpen] = useState(false);
  const [draftMsg, setDraftMsg] = useState("");
  const [draftError, setDraftError] = useState("");
  const displayName = session?.displayName || session?.subject || session?.mode || "—";
  const opCount = draft.operations?.length || 0;

  async function onOpen() {
    setDraftError("");
    try {
      await openDraft({ packageCode: packageCode || undefined });
      setDraftMsg("");
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function onComplete() {
    setDraftError("");
    try {
      const id = await commitDraft();
      setDraftMsg(id ? `${t("changeset.committed")}: ${id}` : t("changeset.committed"));
      setOpsOpen(false);
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function onCancel() {
    setDraftError("");
    try {
      await closeDraft();
      setDraftMsg("");
      setOpsOpen(false);
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <NavLink to="/" className="brand" end>
          {t("appName")}
        </NavLink>

        <div className="nav-section">
          <h2>{t("nav.data")}</h2>
          <NavLink to="/search">{t("nav.search")}</NavLink>
          <NavLink to="/entities">{t("nav.entities")}</NavLink>
        </div>

        <div className="nav-section">
          <h2>{t("nav.model")}</h2>
          <NavLink to="/model/shapes">{t("nav.shapes")}</NavLink>
          <NavLink to="/model/packages">{t("nav.packages")}</NavLink>
          <NavLink to="/model/lenses">{t("nav.lenses")}</NavLink>
          <NavLink to="/model/policies">{t("nav.policies")}</NavLink>
        </div>

        <div className="nav-section">
          <h2>{t("nav.admin")}</h2>
          <NavLink to="/admin/auth">{t("nav.auth")}</NavLink>
          <NavLink to="/admin/schema">{t("nav.schema")}</NavLink>
          <NavLink to="/admin/ops">{t("nav.ops")}</NavLink>
        </div>

        <div style={{ marginTop: "auto" }} className="stack">
          <label className="field">
            {t("package.current")}
            <select value={packageCode} onChange={(e) => setPackageCode(e.target.value)} required>
              {packages.length === 0 && <option value="">{t("package.none")}</option>}
              {packages.map((p) => (
                <option key={p.code} value={p.code}>
                  {p.code}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            {t("common.language")}
            <select
              value={i18n.language.startsWith("cs") ? "cs" : "en"}
              onChange={(e) => {
                void i18n.changeLanguage(e.target.value);
                localStorage.setItem("kc.lang", e.target.value);
              }}
            >
              <option value="cs">Čeština</option>
              <option value="en">English</option>
            </select>
          </label>
          <div className="pill">
            {t("common.you")}: {displayName}
          </div>
          <button type="button" onClick={() => setSession(null)}>
            {t("nav.logout")}
          </button>
        </div>
      </aside>
      <main className="main">
        <div className="draft-banner panel row" style={{ marginBottom: "1rem" }}>
          {isOpen ? (
            <>
              <span className="pill draft-pill">
                {t("changeset.openStatus", { count: opCount })}
              </span>
              <button type="button" className="primary" onClick={() => void onComplete()} disabled={opCount === 0}>
                {t("changeset.complete")}
              </button>
              <button type="button" className="danger" onClick={() => void onCancel()}>
                {t("changeset.cancel")}
              </button>
              <button type="button" onClick={() => setOpsOpen((v) => !v)}>
                {opsOpen ? t("changeset.hideOps") : t("changeset.showOps")}
              </button>
            </>
          ) : (
            <button type="button" onClick={() => void onOpen()}>
              {t("changeset.open")}
            </button>
          )}
          {lastCommittedId && !isOpen && (
            <span className="muted">
              {t("changeset.lastCommitted")}: <code>{lastCommittedId}</code>
            </span>
          )}
        </div>
        {opsOpen && isOpen && (
          <div className="panel stack" style={{ marginBottom: "1rem" }}>
            <h3>{t("changeset.operations")}</h3>
            {opCount === 0 ? (
              <p className="muted">{t("changeset.noOps")}</p>
            ) : (
              <ol className="draft-ops">
                {draft.operations.map((op, i) => (
                  <li key={op.clientKey || `${op.op}-${i}`}>
                    <code>{op.op}</code>
                    {op.entity ? ` · ${op.entity}` : ""}
                    {op.subject ? ` · ${op.subject}` : ""}
                    {op.statement ? ` · ${op.statement}` : ""}
                    {op.property ? ` · ${op.property}` : ""}
                    {op.packageCode ? ` · ${op.packageCode}` : ""}
                  </li>
                ))}
              </ol>
            )}
          </div>
        )}
        {draftMsg && <p className="muted">{draftMsg}</p>}
        {draftError && <p className="error">{draftError}</p>}
        <Outlet />
      </main>
    </div>
  );
}
