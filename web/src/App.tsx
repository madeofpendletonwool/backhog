import { Navigate, Route, Routes } from "react-router-dom";

import { Layout } from "./components/Layout";
import { Spinner } from "./components/ui/primitives";
import { useArena } from "./hooks/useArena";
import { useAuth } from "./hooks/useAuth";
import { AchievementsPage } from "./pages/AchievementsPage";
import { AdminPage } from "./pages/AdminPage";
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
        <Route path="/" element={<Home />} />
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
        {/* The books arena mirrors the games one: the dashboard at the
            arena's root, the shelf one segment under it. The router ranks the
            static segments above the dynamic one, so /books/shelf and
            /books/files stay themselves rather than resolving as entry ids. */}
        <Route path="/books" element={<ReadingDashboardPage />} />
        <Route path="/books/shelf" element={<BookLibraryPage />} />
        <Route path="/books/files" element={<BookFilesPage />} />
        {/* The dashboard's old address, kept for bookmarks. */}
        <Route path="/books/dashboard" element={<Navigate to="/books" replace />} />
        <Route path="/books/:entryId" element={<BookDetailPage />} />
        <Route path="/books/:entryId/read" element={<BookReaderPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        {/* The page turns a non-admin away itself; the API refuses them
            regardless. */}
        <Route path="/admin" element={<AdminPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

/**
 * The root URL. It is the games dashboard, but it is also what you type to
 * open the app, and the app should open where you left it: in the books
 * arena the root hands off to /books rather than flipping you into games.
 */
function Home() {
  const { arena } = useArena();
  return arena === "books" ? <Navigate to="/books" replace /> : <DashboardPage />;
}
