import { cn } from "@/lib/cn";
import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";

import { AchievementToasts } from "./AchievementToasts";
import { AddBookDialog } from "./AddBookDialog";
import { AddGameDialog } from "./AddGameDialog";
import { AudioPlayer } from "./AudioPlayer";
import { ErrorBoundary } from "./ErrorBoundary";
import { PickDialog } from "./PickDialog";
import { ReadDialog } from "./ReadDialog";
import { SteamImportDialog } from "./SteamImportDialog";
import { Button, Gi } from "./ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { useArena } from "@/hooks/useArena";
import { useEggUnlock } from "@/hooks/useAchievements";
import { useBookStats } from "@/hooks/useBooks";
import { AudioPlayerProvider } from "@/hooks/useAudioPlayer";
import { useTheme, type ThemeFamily } from "@/hooks/useTheme";
import { useLists } from "@/hooks/useLists";
import { useStats } from "@/hooks/useLibrary";
import { ruleSetTarget } from "@/lib/smartlists";
import type { Arena } from "@/lib/arena";
import type { GiName } from "@/lib/gameicons";

type NavItem = { to: string; label: string; icon: GiName; end: boolean };

/* The three places the sidebar has to know which family it is in. Records
   rather than ternaries: with two branches a third family lands in whichever
   one happens to be the `else`, and for the kbd hint that is not cosmetic —
   it decides whether the label is readable on the button under it. */

const wordmarkClass: Record<ThemeFamily, string> = {
  pixel: "font-display text-[13px] font-bold uppercase tracking-widest",
  flat: "text-[15px] font-semibold tracking-tight",
  library: "font-display text-[17px] tracking-tight",
};

const sublineClass: Record<ThemeFamily, string> = {
  pixel: "font-display text-[9px] uppercase tracking-wider",
  flat: "text-[11px]",
  library: "font-display text-[12px] italic",
};

/* The shortcut hint sits *on* the primary button, so it has to read against
   whatever that button is: dark ink on the arcade's gold and the library's
   ember, light ink on Midnight's violet. */
const kbdClass: Record<ThemeFamily, string> = {
  pixel: "rounded-[2px] bg-black/20 text-black/60",
  flat: "rounded border border-white/20 text-white/70",
  library: "rounded-xs bg-black/15 text-black/55",
};

const gameNav: NavItem[] = [
  { to: "/", label: "Dashboard", icon: "gauge", end: true },
  { to: "/library", label: "Library", icon: "layout-grid", end: true },
  { to: "/queue", label: "Play Queue", icon: "list-ordered", end: false },
  { to: "/debt", label: "Backlog Debt", icon: "hourglass", end: false },
  { to: "/series", label: "Series", icon: "layers", end: false },
  { to: "/achievements", label: "Achievements", icon: "trophy", end: false },
  { to: "/lists", label: "Lists", icon: "list-tree", end: false },
  { to: "/projects", label: "Projects", icon: "target", end: false },
];

/* The shared pages carry ?media=book from here, so arriving from the books
   nav lands on the books half of a page that holds both. */
const bookNav: NavItem[] = [
  { to: "/books/dashboard", label: "Dashboard", icon: "gauge", end: true },
  { to: "/books", label: "Shelf", icon: "layout-grid", end: true },
  { to: "/queue?media=book", label: "Reading Queue", icon: "list-ordered", end: false },
  { to: "/debt?media=book", label: "Reading Debt", icon: "hourglass", end: false },
  { to: "/books/files", label: "Book files", icon: "full-folder", end: false },
  { to: "/lists", label: "Lists", icon: "list-tree", end: false },
  { to: "/projects", label: "Projects", icon: "target", end: false },
];

export function Layout() {
  const [addOpen, setAddOpen] = useState(false);
  const [pickOpen, setPickOpen] = useState(false);
  const [readOpen, setReadOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const { data: listData } = useLists();
  const { family } = useTheme();
  const { arena, switchArena } = useArena();
  const fireEgg = useEggUnlock();

  const { data: stats } = useStats();
  const { data: bookStats } = useBookStats(arena === "books");

  const navItems = arena === "books" ? bookNav : gameNav;

  /* The hog is the mark in Midnight, the joystick in the arcade — the
     one place a component gets to know which family it is in, because a
     bitmap sprite and an emoji are not interchangeable in CSS. */
  const mark = (size: "sm" | "lg") =>
    family === "pixel" ? (
      // Whole-number scales only, so the arcade mark is 32px in both
      // slots; it is the emoji that changes size between them.
      <span className="mark-hog" />
    ) : (
      <span className={size === "lg" ? "text-2xl leading-none" : "text-xl leading-none"}>🐗</span>
    );

  // The logo is watching for watchers: ten clicks on the hog and the
  // Hog Watcher egg hatches. The counter resets on reload — a streak
  // should be one sitting.
  const logoClicks = useRef(0);
  const onLogoClick = () => {
    logoClicks.current += 1;
    if (logoClicks.current >= 10) {
      logoClicks.current = 0;
      void fireEgg("hog_watcher");
    }
  };

  // Cmd/Ctrl+K opens the add dialog from anywhere in the app.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setAddOpen(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // A smart list files itself under the arena its rules target; an unscoped
  // set matches both arenas and shows in both. The books nav therefore never
  // surfaces the games arena's lists wearing the wrong words.
  const arenaMedia = arena === "books" ? "book" : "game";
  const smartLists =
    listData?.lists.filter(
      (list) =>
        list.kind === "smart" &&
        (ruleSetTarget(list.rules) === null || ruleSetTarget(list.rules) === arenaMedia),
    ) ?? [];

  /* The whole app lives inside the audio player's provider, so the one
     <audio> element survives every navigation; the player bar itself is
     fixed-positioned and hides itself when nothing is loaded. */
  const chrome = (
    <div className="flex min-h-screen">
      <aside className="f-panel fixed inset-y-2 left-2 z-20 hidden w-60 flex-col px-3 py-5 lg:flex">
        <div
          className="flex items-center gap-3 px-2 pb-6 select-none"
          title="Backhog"
          onClick={onLogoClick}
        >
          {mark("lg")}
          <div>
            <p className={cn("text-ink-100", wordmarkClass[family])}>Backhog</p>
            <p className={cn("mt-1 text-ink-400", sublineClass[family])}>
              {arena === "books"
                ? bookStats
                  ? `${bookStats.backlog} left to read`
                  : "\u00a0"
                : stats
                  ? `${stats.backlog} in the backlog`
                  : "\u00a0"}
            </p>
          </div>
        </div>

        <ArenaSwitch arena={arena} onSwitch={switchArena} className="mb-4" />

        <Button variant="primary" className="mb-4 w-full" onClick={() => setAddOpen(true)}>
          <Gi name="plus" className="size-3.5" />
          {arena === "books" ? "Add book" : "Add game"}
          <kbd
            className={cn(
              "ml-auto px-1.5 py-0.5 font-sans text-[10px] font-normal normal-case tracking-normal",
              kbdClass[family],
            )}
          >
            ⌘K
          </kbd>
        </Button>

        {/* Same anti-deliberation device on both sides of the house — the
            arena only decides which length the picks reason about. */}
        <Button
          variant="secondary"
          className="mb-6 w-full"
          onClick={() => (arena === "books" ? setReadOpen(true) : setPickOpen(true))}
        >
          <Gi name="dices" className="size-3.5" />
          {arena === "books" ? "What should I read?" : "What should I play?"}
        </Button>

        <nav className="space-y-1">
          {navItems.map(({ to, label, icon, end }) => (
            <NavLink key={to} to={to} end={end} className={navLinkClass}>
              <Gi name={icon} className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>

        {smartLists.length > 0 && (
          <div className="mt-7">
            <p className="px-2 pb-2 font-display text-[9px] font-bold uppercase tracking-widest text-ink-400">
              Smart lists
            </p>
            <div className="space-y-1">
              {smartLists.map((list) => (
                <NavLink key={list.id} to={`/lists/${list.id}`} className={navLinkClass}>
                  <Gi name="sparkles" className="size-3.5 shrink-0 text-hl-bright" />
                  <span className="truncate">{list.name}</span>
                  <span className="ml-auto shrink-0 font-display text-[10px] tabular-nums text-ink-400">
                    {list.count}
                  </span>
                </NavLink>
              ))}
            </div>
          </div>
        )}

        <div className="mt-auto space-y-1 border-t-2 border-line pt-3">
          {arena === "games" && (
            <button onClick={() => setImportOpen(true)} className={actionLinkClass}>
              <Gi name="download" className="size-4" />
              Import from Steam
            </button>
          )}
          <NavLink to="/settings" className={navLinkClass}>
            <Gi name="settings" className="size-4" />
            <span className="truncate">{user?.username}</span>
          </NavLink>
          <button
            onClick={async () => {
              await logout();
              navigate("/login");
            }}
            className="flex w-full items-center gap-2.5 px-3 py-2 text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
          >
            <Gi name="log-out" className="size-4" />
            Sign out
          </button>
        </div>
      </aside>

      {/* Mobile top bar; the sidebar collapses away below lg. */}
      <header className="f-panel fixed inset-x-2 top-2 z-30 flex items-center gap-1 px-3 py-1.5 lg:hidden">
        <button type="button" aria-label="Backhog" className="shrink-0" onClick={onLogoClick}>
          {mark("sm")}
        </button>
        <ArenaSwitch arena={arena} onSwitch={switchArena} compact />
        <nav className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {navItems.map(({ to, label, icon, end }) => (
            <NavLink key={to} to={to} end={end} className={mobileLinkClass} title={label}>
              <Gi name={icon} className="size-4" />
            </NavLink>
          ))}
          <NavLink to="/settings" className={mobileLinkClass} title="Settings">
            <Gi name="settings" className="size-4" />
          </NavLink>
        </nav>
        <button
          onClick={() => (arena === "books" ? setReadOpen(true) : setPickOpen(true))}
          className="shrink-0 p-2 text-ink-400 transition-colors hover:text-ink-100"
          aria-label={arena === "books" ? "What should I read?" : "What should I play?"}
        >
          <Gi name="dices" className="size-4" />
        </button>
        <Button size="sm" variant="primary" className="shrink-0" onClick={() => setAddOpen(true)}>
          <Gi name="plus" className="size-3.5" />
          Add
        </Button>
      </header>

      {/* The player bar publishes its own height as --player-h (0 when no
          book is open), so the last row of a page is never buried under it. */}
      <main className="min-w-0 flex-1 pb-[var(--player-h,0px)] pt-20 lg:pl-[17rem] lg:pt-0">
        <ErrorBoundary>
          <Outlet context={{ openAddDialog: () => setAddOpen(true) }} />
        </ErrorBoundary>
      </main>

      <AddGameDialog open={addOpen && arena === "games"} onClose={() => setAddOpen(false)} />
      <AddBookDialog open={addOpen && arena === "books"} onClose={() => setAddOpen(false)} />
      <PickDialog open={pickOpen} onClose={() => setPickOpen(false)} />
      <ReadDialog open={readOpen} onClose={() => setReadOpen(false)} />
      <SteamImportDialog open={importOpen} onClose={() => setImportOpen(false)} />
      <AchievementToasts />
    </div>
  );

  return (
    <AudioPlayerProvider>
      {chrome}
      <AudioPlayer />
    </AudioPlayerProvider>
  );
}

/**
 * The mode toggle. Two segments, always both visible: the point is that the
 * other arena exists and is one click away, which a dropdown would hide.
 */
function ArenaSwitch({
  arena,
  onSwitch,
  className,
  compact = false,
}: {
  arena: Arena;
  onSwitch: (next: Arena) => void;
  className?: string;
  compact?: boolean;
}) {
  const segments: { value: Arena; label: string; icon: GiName }[] = [
    { value: "games", label: "Games", icon: "gamepad" },
    { value: "books", label: "Books", icon: "book-pile" },
  ];

  return (
    <div
      role="group"
      aria-label="Arena"
      className={cn(
        "flex shrink-0 rounded-xl border border-edge bg-ink-850 p-0.5",
        className,
      )}
    >
      {segments.map(({ value, label, icon }) => (
        <button
          key={value}
          type="button"
          onClick={() => onSwitch(value)}
          aria-pressed={arena === value}
          aria-label={compact ? label : undefined}
          title={compact ? label : undefined}
          className={cn(
            "flex items-center justify-center gap-1.5 rounded-[0.6rem] transition-colors focus-visible:focus-ring",
            compact ? "p-2" : "flex-1 px-2 py-1.5 text-xs font-medium",
            arena === value ? "bg-fill-active text-ink-100" : "text-ink-500 hover:text-ink-300",
          )}
        >
          <Gi name={icon} className="size-4" />
          {!compact && label}
        </button>
      ))}
    </div>
  );
}

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "flex items-center gap-2.5 px-3 py-2 text-sm transition-colors focus-visible:focus-ring",
    isActive
      ? "f-panel-active font-medium text-ink-100"
      : "text-ink-400 hover:text-ink-200",
  );

const actionLinkClass =
  "flex w-full items-center gap-2.5 px-3 py-2 text-sm text-ink-400 transition-colors hover:text-ink-200 focus-visible:focus-ring";

const mobileLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn("p-2 transition-colors", isActive ? "text-hl-bright" : "text-ink-400");
