import { Navigate, Route, Routes } from "react-router-dom";

import { Layout } from "./components/Layout";
import { Spinner } from "./components/ui/primitives";
import { useAuth } from "./hooks/useAuth";
import { GameDetailPage } from "./pages/GameDetailPage";
import { LibraryPage } from "./pages/LibraryPage";
import { ListDetailPage } from "./pages/ListDetailPage";
import { ListsPage } from "./pages/ListsPage";
import { LoginPage } from "./pages/LoginPage";
import { QueuePage } from "./pages/QueuePage";
import { RegisterPage } from "./pages/RegisterPage";
import { SettingsPage } from "./pages/SettingsPage";

export function App() {
  const { user, loading } = useAuth();

  // Hold routing until the session check resolves, so an authenticated reload
  // never flashes the login page.
  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner className="size-6" />
      </div>
    );
  }

  if (!user) {
    return (
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    );
  }

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<LibraryPage />} />
        <Route path="/queue" element={<QueuePage />} />
        <Route path="/lists" element={<ListsPage />} />
        <Route path="/lists/:listId" element={<ListDetailPage />} />
        <Route path="/game/:entryId" element={<GameDetailPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
