import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { Layout } from "./Layout";
import { PackageProvider } from "./package";
import { ChangeSetDraftProvider } from "./changeset";
import { LoginPage } from "./pages/LoginPage";
import { OidcCallbackPage } from "./pages/OidcCallbackPage";
import { SearchPage } from "./pages/SearchPage";
import { EntitiesPage } from "./pages/EntitiesPage";
import { EntityPage } from "./pages/EntityPage";
import { EntityHistoryPage } from "./pages/EntityHistoryPage";
import { PackagesPage } from "./pages/PackagesPage";
import { PackageDetailPage } from "./pages/PackageDetailPage";
import { StatementPage } from "./pages/StatementPage";
import { ReferencePage } from "./pages/ReferencePage";
import { LensesPage } from "./pages/LensesPage";
import { PoliciesPage } from "./pages/PoliciesPage";
import { OpsPage } from "./pages/OpsPage";
import { ShapesPage } from "./pages/ShapesPage";
import { ClassPropertiesPage } from "./pages/ClassPropertiesPage";
import { SchemaConfigPage } from "./pages/SchemaConfigPage";
import { ValidationPage } from "./pages/ValidationPage";
import { ChangesetsPage } from "./pages/ChangesetsPage";
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
            <PackageProvider>
              <ChangeSetDraftProvider>
                <Layout />
              </ChangeSetDraftProvider>
            </PackageProvider>
          </RequireAuth>
        }
      >
        <Route index element={<Navigate to="/entities" replace />} />
        <Route path="search" element={<SearchPage />} />
        <Route path="entities" element={<EntitiesPage />} />
        <Route path="changesets" element={<ChangesetsPage />} />
        <Route path="changesets/:cid" element={<ChangesetsPage />} />
        <Route path="entities/:qid/validation" element={<ValidationPage />} />
        <Route path="entities/:qid/history" element={<EntityHistoryPage />} />
        <Route path="entities/:qid" element={<EntityPage />} />
        <Route path="statements/:sid" element={<StatementPage />} />
        <Route path="references/:rid" element={<ReferencePage />} />
        <Route path="model/properties" element={<Navigate to="/entities?kind=property" replace />} />
        <Route path="model/classes" element={<Navigate to="/entities?kind=class" replace />} />
        <Route path="model/shapes" element={<ShapesPage />} />
        <Route path="model/class-properties" element={<ClassPropertiesPage />} />
        <Route path="model/packages" element={<PackagesPage />} />
        <Route path="model/packages/:code" element={<PackageDetailPage />} />
        <Route path="model/lenses" element={<LensesPage />} />
        <Route path="model/policies" element={<PoliciesPage />} />
        <Route path="admin/auth" element={<AuthPage />} />
        <Route path="admin/schema" element={<SchemaConfigPage />} />
        <Route path="admin/ops" element={<OpsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
