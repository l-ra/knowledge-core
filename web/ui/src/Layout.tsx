import { Link, NavLink, Outlet } from "react-router-dom";
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
  const {
    isOpen,
    active,
    openChangeSet,
    leaveChangeSet,
    cancelChangeSet,
    commitChangeSet,
    lastCommittedId,
  } = useChangeSetDraft();
  const [draftMsg, setDraftMsg] = useState("");
  const [draftError, setDraftError] = useState("");
  const [hideObjectIds, setHideObjectIds] = useState(() => localStorage.getItem("kc.hideObjectIds") === "1");
  const displayName = session?.displayName || session?.subject || session?.mode || "—";

  function toggleObjectIds() {
    setHideObjectIds((prev) => {
      const next = !prev;
      localStorage.setItem("kc.hideObjectIds", next ? "1" : "0");
      return next;
    });
  }

  async function onOpen() {
    setDraftError("");
    try {
      await openChangeSet();
      setDraftMsg("");
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  async function onComplete() {
    setDraftError("");
    try {
      const id = await commitChangeSet();
      setDraftMsg(id ? `${t("changeset.committed")}: ${id}` : t("changeset.committed"));
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  function onLeave() {
    setDraftError("");
    leaveChangeSet();
    setDraftMsg(t("changeset.deferred"));
  }

  async function onCancel() {
    setDraftError("");
    try {
      await cancelChangeSet();
      setDraftMsg("");
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : t("common.error"));
    }
  }

  return (
    <div className={`app-shell${hideObjectIds ? " ids-hidden" : ""}`}>
      <aside className="sidebar">
        <NavLink to="/" className="brand" end>
          {t("appName")}
        </NavLink>

        <div className="nav-section">
          <h2>{t("nav.data")}</h2>
          <NavLink to="/search">{t("nav.search")}</NavLink>
          <NavLink to="/entities">{t("nav.entities")}</NavLink>
          <NavLink to="/changesets">{t("nav.changesets")}</NavLink>
        </div>

        <div className="nav-section">
          <h2>{t("nav.model")}</h2>
          <NavLink to="/model/shapes">{t("nav.shapes")}</NavLink>
          <NavLink to="/model/class-properties">{t("nav.classProperties")}</NavLink>
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
      </aside>

      <div className="main-area">
        <header className="topbar">
          <div className="topbar-group">
            <select
              className="topbar-select"
              value={packageCode}
              onChange={(e) => setPackageCode(e.target.value)}
              title={t("package.current")}
            >
              {packages.length === 0 && <option value="">{t("package.none")}</option>}
              {packages.map((p) => (
                <option key={p.code} value={p.code}>
                  {p.code}
                </option>
              ))}
            </select>

            <div className="topbar-divider" />

            {isOpen ? (
              <>
                <span className="topbar-badge">
                  {t("changeset.openStatus", { id: active?.id || "" })}
                </span>
                <button type="button" className="topbar-btn primary" onClick={() => void onComplete()} title={t("changeset.completeHint")}>
                  {t("changeset.complete")}
                </button>
                <button type="button" className="topbar-btn" onClick={onLeave} title={t("changeset.deferHint")}>
                  {t("changeset.defer")}
                </button>
                <button type="button" className="topbar-btn danger" onClick={() => void onCancel()} title={t("changeset.cancelHint")}>
                  {t("changeset.cancel")}
                </button>
                {active?.id && (
                  <Link className="topbar-muted" to={`/changesets/${encodeURIComponent(active.id)}`}>
                    <code>{active.id}</code>
                  </Link>
                )}
              </>
            ) : (
              <>
                <button type="button" className="topbar-btn" onClick={() => void onOpen()}>
                  {t("changeset.open")}
                </button>
                {lastCommittedId && (
                  <span className="topbar-muted">
                    {t("changeset.lastCommitted")}:{" "}
                    <Link to={`/changesets/${encodeURIComponent(lastCommittedId)}`}>
                      <code>{lastCommittedId}</code>
                    </Link>
                  </span>
                )}
              </>
            )}
          </div>

          <div className="topbar-group">
            <select
              className="topbar-select topbar-select-sm"
              value={i18n.language.startsWith("cs") ? "cs" : "en"}
              onChange={(e) => {
                void i18n.changeLanguage(e.target.value);
                localStorage.setItem("kc.lang", e.target.value);
              }}
              title={t("common.language")}
            >
              <option value="cs">CS</option>
              <option value="en">EN</option>
            </select>

            <button type="button" className="topbar-btn" onClick={toggleObjectIds}>
              {hideObjectIds ? t("common.showLocalIds") : t("common.hideLocalIds")}
            </button>

            <div className="topbar-divider" />

            <span className="topbar-user" title={displayName}>
              {displayName}
            </span>
            <button type="button" className="topbar-btn" onClick={() => setSession(null)}>
              {t("nav.logout")}
            </button>
          </div>
        </header>

        {draftMsg && <p className="muted" style={{ padding: "0 clamp(1rem, 3vw, 2.5rem)" }}>{draftMsg}</p>}
        {draftError && <p className="error" style={{ padding: "0 clamp(1rem, 3vw, 2.5rem)" }}>{draftError}</p>}

        <main className="main">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
