import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import { App } from "./App";
import { Scene } from "./components/Scene";
import { ArenaProvider } from "./hooks/useArena";
import { AuthProvider } from "./hooks/useAuth";
import { ThemeProvider } from "./hooks/useTheme";
import { ApiError } from "./lib/api";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Retrying a 401 just delays the redirect to the login page.
      retry: (failureCount, error) =>
        !(error instanceof ApiError && error.status < 500) && failureCount < 2,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      {/* The theme follows the arena, and the arena follows the URL, so both
          providers have to sit inside the router. Nothing above ThemeProvider
          consumes the theme, so it loses nothing by moving down here. */}
      <BrowserRouter>
        <ArenaProvider>
          <ThemeProvider>
            <AuthProvider>
              <Scene />
              <App />
            </AuthProvider>
          </ThemeProvider>
        </ArenaProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
