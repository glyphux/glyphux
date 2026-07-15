import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AuthProvider } from "@/lib/auth-context";
import { ThemeProvider } from "@/lib/theme-context";
import { ToastProvider } from "@/lib/toast-context";
import { ProtectedRoute } from "@/components/layout/ProtectedRoute";
import { AppShell } from "@/components/layout/AppShell";
import { LoginPage } from "@/pages/LoginPage";
import { DashboardPage } from "@/pages/DashboardPage";
import { ContentTypesListPage } from "@/pages/content-types/ContentTypesListPage";
import { ContentTypeFormPage } from "@/pages/content-types/ContentTypeFormPage";
import { ContentListPage } from "@/pages/content/ContentListPage";
import { ContentFormPage } from "@/pages/content/ContentFormPage";
import { MediaLibraryPage } from "@/pages/media/MediaLibraryPage";
import { UsersPage } from "@/pages/users/UsersPage";

function App() {
  return (
    <ThemeProvider>
      <ToastProvider>
        {/* basename "/admin" — the SPA is embedded and served under that
         * prefix by internal/adminui; every route below is relative to it. */}
        <BrowserRouter basename="/admin">
          <AuthProvider>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route element={<ProtectedRoute />}>
                <Route element={<AppShell />}>
                  <Route index element={<DashboardPage />} />
                  <Route path="content-types" element={<ContentTypesListPage />} />
                  <Route path="content-types/new" element={<ContentTypeFormPage />} />
                  <Route path="content-types/:name/edit" element={<ContentTypeFormPage />} />
                  <Route path="content/:type" element={<ContentListPage />} />
                  <Route path="content/:type/new" element={<ContentFormPage />} />
                  <Route path="content/:type/:id" element={<ContentFormPage />} />
                  <Route path="media" element={<MediaLibraryPage />} />
                  <Route path="users" element={<UsersPage />} />
                </Route>
              </Route>
            </Routes>
          </AuthProvider>
        </BrowserRouter>
      </ToastProvider>
    </ThemeProvider>
  );
}

export default App;
