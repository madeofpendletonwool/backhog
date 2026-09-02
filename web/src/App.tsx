import { Navigate, Route, Routes } from "react-router-dom";

import { Layout } from "./components/Layout";
import { Spinner } from "./components/ui/primitives";
import { useAuth } from "./hooks/useAuth";
import { AchievementsPage } from "./pages/AchievementsPage";
import { BookDetailPage } from "./pages/BookDetailPage";
import { BookFilesPage } from "./pages/BookFilesPage";
import { BookLibraryPage } from "./pages/BookLibraryPage";
import { BookReaderPage } from "./pages/BookReaderPage";
import { DashboardPage } from "./pages/DashboardPage";
import { DebtPage } from "./pages/DebtPage";
import { GameDetailPage } from "./pages/GameDetailPage";
import { LibraryPage } from "./pages/LibraryPage";
import { ListDetailPage } from "./pages/ListDetailPage";
import { ListsPage } from "./pages/ListsPage";
import { LoginPage } from "./pages/LoginPage";
import { ReadingDashboardPage } from "./pages/ReadingDashboardPage";
import { ProjectDetailPage } from "./pages/ProjectDetailPage";
import { ProjectsPage } from "./pages/ProjectsPage";
import { QueuePage } from "./pages/QueuePage";
import { RegisterPage } from "./pages/RegisterPage";
import { SeriesDetailPage } from "./pages/SeriesDetailPage";
import { SeriesPage } from "./pages/SeriesPage";
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
        <Route path="/" element={<DashboardPage />} />
        <Route path="/library" element={<LibraryPage />} />
        <Route path="/queue" element={<QueuePage />} />
        <Route path="/debt" element={<DebtPage />} />
        <Route path="/lists" element={<ListsPage />} />
        <Route path="/lists/:listId" element={<ListDetailPage />} />
        <Route path="/projects" element={<ProjectsPage />} />
        <Route path="/projects/:projectId" element={<ProjectDetailPage />} />
        <Route path="/series" element={<SeriesPage />} />
        <Route path="/series/:seriesId" element={<SeriesDetailPage />} />
        <Route path="/achievements" element={<AchievementsPage />} />
        <Route path="/game/:entryId" element={<GameDetailPage />} />
        <Route path="/books" element={<BookLibraryPage />} />
        {/* The router ranks the static segments above the dynamic one, so
            /books/files and /books/dashboard stay themselves rather than
            resolving as entry ids. */}
        <Route path="/books/files" element={<BookFilesPage />} />
        <Route path="/books/dashboard" element={<ReadingDashboardPage />} />
        <Route path="/books/:entryId" element={<BookDetailPage />} />
        <Route path="/books/:entryId/read" element={<BookReaderPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
