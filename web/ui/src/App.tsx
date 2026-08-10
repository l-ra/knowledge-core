import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { Layout } from "./Layout";
import { LoginPage } from "./pages/LoginPage";
import { OidcCallbackPage } from "./pages/OidcCallbackPage";
import { SearchPage } from "./pages/SearchPage";
import { EntitiesPage } from "./pages/EntitiesPage";
import { EntityPage } from "./pages/EntityPage";
import { EntityHistoryPage } from "./pages/EntityHistoryPage";
import { PropertiesPage } from "./pages/PropertiesPage";
import { PackagesPage } from "./pages/PackagesPage";
import { LensesPage } from "./pages/LensesPage";
import { PoliciesPage } from "./pages/PoliciesPage";
import { OpsPage } from "./pages/OpsPage";
import { AuthPage } from "./pages/AuthPage";

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { session, ready } = useAuth();
  if (!ready) return null;
  if (!session) return <Navigate to="/login" replace />;
  return children;
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/callback" element={<OidcCallbackPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<Navigate to="/search" replace />} />
        <Route path="search" element={<SearchPage />} />
        <Route path="entities" element={<EntitiesPage />} />
        <Route path="entities/:qid" element={<EntityPage />} />
        <Route path="entities/:qid/history" element={<EntityHistoryPage />} />
        <Route path="model/properties" element={<PropertiesPage />} />
        <Route path="model/packages" element={<PackagesPage />} />
        <Route path="model/lenses" element={<LensesPage />} />
        <Route path="model/policies" element={<PoliciesPage />} />
        <Route path="admin/auth" element={<AuthPage />} />
        <Route path="admin/ops" element={<OpsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
