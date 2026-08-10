import { NavLink, Outlet } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "./auth";
import i18n from "./i18n";

export function Layout() {
  const { t } = useTranslation();
  const { session, setSession } = useAuth();

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
          <NavLink to="/model/properties">{t("nav.properties")}</NavLink>
          <NavLink to="/model/packages">{t("nav.packages")}</NavLink>
          <NavLink to="/model/lenses">{t("nav.lenses")}</NavLink>
          <NavLink to="/model/policies">{t("nav.policies")}</NavLink>
        </div>

        <div className="nav-section">
          <h2>{t("nav.admin")}</h2>
          <NavLink to="/admin/ops">{t("nav.ops")}</NavLink>
        </div>

        <div style={{ marginTop: "auto" }} className="stack">
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
            {t("common.you")}: {session?.subject || session?.mode}
          </div>
          <button type="button" onClick={() => setSession(null)}>
            {t("nav.logout")}
          </button>
        </div>
      </aside>
      <main className="main">
        <Outlet />
      </main>
    </div>
  );
}
