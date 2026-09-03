/**
 * Backhog has two arenas. They share a database, a queue, lists and projects,
 * and every piece of chrome on the page — what a mode swap changes is the nav
 * set, the route prefix, the verb on the primary button, and (since themes
 * went per-arena) the whole look of the room. Games keep the URLs they have
 * always had; books live under /books.
 *
 * The route table is *data* rather than a chain of ifs for one reason: the
 * pre-paint script in index.html has to answer the same question before React
 * exists, and a second hand-written copy of that logic would drift. The Vite
 * plugin in vite.config.ts serialises `ARENA_ROUTES` into the boot script, so
 * this file is the only place the mapping is written down.
 *
 * Keep this module dependency-free — the Vite config imports it at build time.
 */

export type Arena = "games" | "books";

export const ARENAS: Arena[] = ["games", "books"];

export const ARENA_LABELS: Record<Arena, string> = { games: "Games", books: "Books" };

export const arenaHome: Record<Arena, string> = { games: "/", books: "/books" };

type Match = "prefix" | "exact";

/**
 * Ordered, but only loosely: the resolver below takes books-by-path first and
 * games-by-path last, so within each arena the order does not matter.
 *
 * `prefix` means the path itself or anything under it — "/series" matches
 * "/series" and "/series/{id}" but not "/seriesfoo".
 */
export const ARENA_ROUTES: { match: Match; path: string; arena: Arena }[] = [
  { match: "prefix", path: "/books", arena: "books" },
  { match: "exact", path: "/", arena: "games" },
  { match: "exact", path: "/library", arena: "games" },
  { match: "exact", path: "/debt", arena: "games" },
  { match: "exact", path: "/achievements", arena: "games" },
  { match: "prefix", path: "/game", arena: "games" },
  { match: "prefix", path: "/series", arena: "games" },
];

function hit(route: { match: Match; path: string }, pathname: string): boolean {
  if (route.match === "exact") return pathname === route.path;
  return pathname === route.path || pathname.startsWith(route.path + "/");
}

/**
 * Which arena a URL belongs to, or null when the page is shared and the answer
 * is "whichever one you came from". Deep links have to win over the remembered
 * mode: a bookmarked /books/{id} must not open wearing the games nav just
 * because the last session ended on a game.
 *
 * Precedence — books by path, then ?media=book, then games by path. The middle
 * rule has to out-rank the last one: /debt is a games path, and /debt?media=book
 * is the books half of a page that holds both.
 */
export function arenaForLocation(pathname: string, search: string): Arena | null {
  const byPath = ARENA_ROUTES.find((route) => hit(route, pathname));
  if (byPath?.arena === "books") return "books";
  if (new URLSearchParams(search).get("media") === "book") return "books";
  return byPath?.arena ?? null;
}
