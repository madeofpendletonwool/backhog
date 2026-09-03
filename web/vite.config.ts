import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

import { ARENA_ROUTES } from "./src/lib/arena";
import { DEFAULT_THEME, THEMES } from "./src/lib/themes";

/**
 * Stamp the saved theme onto <html> before the first paint.
 *
 * Without this the page renders one frame of the CSS defaults (the arcade
 * cabinet) before React mounts and corrects it, which reads as a flash of the
 * wrong app. Since themes went per-arena the script has to work out the arena
 * first, so it needs the route table — and a second hand-written copy of that
 * table would drift from the real one.
 *
 * So it is generated: the route table and the id -> family map are serialised
 * out of lib/arena.ts and lib/themes.ts, which are the modules the app itself
 * uses. Both are kept dependency-free so importing them here costs nothing.
 */
function bootTheme(): Plugin {
  const families = Object.fromEntries(
    Object.entries(THEMES).map(([id, meta]) => [id, meta.family]),
  );

  const script = `<script>
(function () {
  var ROUTES = ${JSON.stringify(ARENA_ROUTES)};
  var FAMILY = ${JSON.stringify(families)};
  var DEFAULT = ${JSON.stringify(DEFAULT_THEME)};

  function hit(r, p) {
    return r.match === "exact" ? p === r.path : p === r.path || p.indexOf(r.path + "/") === 0;
  }
  function arenaFor(pathname, search) {
    var byPath = null;
    for (var i = 0; i < ROUTES.length; i++) {
      if (hit(ROUTES[i], pathname)) { byPath = ROUTES[i].arena; break; }
    }
    if (byPath === "books") return "books";
    if (new URLSearchParams(search).get("media") === "book") return "books";
    return byPath;
  }

  var theme = DEFAULT, family = FAMILY[DEFAULT];
  try {
    var arena = arenaFor(location.pathname, location.search);
    if (!arena) {
      // Shared page: whichever arena you were last in. usePersistentState
      // writes JSON, so this value arrives quoted.
      var saved = localStorage.getItem("backhog:arena");
      try { saved = JSON.parse(saved); } catch (e) { /* pre-JSON value */ }
      arena = saved === "books" ? "books" : "games";
    }

    var slot = localStorage.getItem("backhog:theme:" + arena);
    if (slot && slot.indexOf(":") > 0) {
      var parts = slot.split(":");
      if (FAMILY[parts[1]]) { family = parts[0]; theme = parts[1]; }
    } else {
      // Pre-per-arena install. useTheme migrates this properly on mount; here
      // we only need the first frame to be right.
      var legacy = localStorage.getItem("backhog:theme");
      if (legacy && FAMILY[legacy]) { theme = legacy; family = FAMILY[legacy]; }
    }
  } catch (e) {
    /* private browsing — fall through to the default */
  }
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.family = family;
})();
</script>`;

  return {
    name: "backhog-boot-theme",
    transformIndexHtml(html) {
      return html.replace("<!--@boot-theme-->", script);
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss(), bootTheme()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    port: 5173,
    // In dev the API runs separately; in production nginx serves both on one
    // origin. Proxying here keeps cookies same-origin in both cases.
    proxy: {
      "/api": {
        target: process.env.VITE_API_TARGET ?? "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
