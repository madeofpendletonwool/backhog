import { cn } from "@/lib/cn";
import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";

import { AchievementToasts } from "./AchievementToasts";
import { AddBookDialog } from "./AddBookDialog";
import { AddGameDialog } from "./AddGameDialog";
import { AudioPlayer } from "./player/AudioPlayer";
import { ErrorBoundary } from "./ErrorBoundary";
import { JumpDialog } from "./JumpDialog";
import { PickDialog } from "./PickDialog";
import { ReadDialog } from "./ReadDialog";
import { SteamImportDialog } from "./SteamImportDialog";
import { Dialog } from "./ui/Dialog";
import { Button, Gi } from "./ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { useArena } from "@/hooks/useArena";
import { useEggUnlock } from "@/hooks/useAchievements";
import { useBookStats, useReadingNow } from "@/hooks/useBooks";
import { useContinueReading } from "@/hooks/useContinueReading";
import { AudioPlayerProvider } from "@/hooks/useAudioPlayer";
import { useTheme, type ThemeFamily } from "@/hooks/useTheme";
import { useLists } from "@/hooks/useLists";
import { useStats } from "@/hooks/useLibrary";
import { ruleSetTarget } from "@/lib/smartlists";
import type { Arena } from "@/lib/arena";
import type { GiName } from "@/lib/gameicons";

type NavItem = {
  to: string;
  label: string;
  icon: GiName;
  end: boolean;
  /** Only for accounts that may manage library files — see useAuth. */
  media?: boolean;
  /**
   * Earns a slot on the phone's tab bar. There are four of those, with
   * labels, and everything else lives behind "More" — a row of nine bare
   * icons was a guessing game.
   */
  primary?: boolean;
};

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

const gameNav: NavItem[] = [
  { to: "/", label: "Dashboard", icon: "gauge", end: true, primary: true },
  { to: "/library", label: "Library", icon: "layout-grid", end: true, primary: true },
  { to: "/queue", label: "Play Queue", icon: "list-ordered", end: false, primary: true },
  { to: "/debt", label: "Backlog Debt", icon: "hourglass", end: false },
  { to: "/series", label: "Series", icon: "layers", end: false, primary: true },
  { to: "/achievements", label: "Achievements", icon: "trophy", end: false },
  { to: "/lists", label: "Lists", icon: "list-tree", end: false },
  { to: "/projects", label: "Projects", icon: "target", end: false },
];

/* The shared pages carry ?media=book from here, so arriving from the books
   nav lands on the books half of a page that holds both. */
const bookNav: NavItem[] = [
  { to: "/books", label: "Dashboard", icon: "gauge", end: true, primary: true },
  { to: "/books/shelf", label: "Shelf", icon: "layout-grid", end: true, primary: true },
  { to: "/queue?media=book", label: "Reading Queue", icon: "list-ordered", end: false, primary: true },
  { to: "/debt?media=book", label: "Reading Debt", icon: "hourglass", end: false },
  // The attach flow. A reader has no business here and the API would refuse
  // them anyway, so the item is dropped from their nav rather than left to
  // lead somewhere that answers 403 — see mediaNav below.
  { to: "/books/files", label: "Book files", icon: "full-folder", end: false, media: true, primary: true },
  { to: "/achievements?media=book", label: "Achievements", icon: "trophy", end: false },
  { to: "/lists", label: "Lists", icon: "list-tree", end: false },
  { to: "/projects", label: "Projects", icon: "target", end: false },
];

export function Layout() {
  const [addOpen, setAddOpen] = useState(false);
  // What the jump palette had typed when it handed off to the add dialog.
  const [addQuery, setAddQuery] = useState("");
  const [jumpOpen, setJumpOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [pickOpen, setPickOpen] = useState(false);
  const [readOpen, setReadOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const { user, logout, canManageMedia, isAdmin } = useAuth();
  const navigate = useNavigate();
  const { data: listData } = useLists();
  const { family } = useTheme();
  const { arena, switchArena } = useArena();
  const fireEgg = useEggUnlock();

  const { data: stats } = useStats();
  const { data: bookStats } = useBookStats(arena === "books");

  const navItems = (arena === "books" ? bookNav : gameNav).filter(
    (item) => !item.media || canManageMedia,
  );
  const primaryItems = navItems.filter((item) => item.primary);
  const moreItems = navItems.filter((item) => !item.primary);

  const openAdd = (query = "") => {
    setAddQuery(query);
    setAddOpen(true);
  };

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

  // Cmd/Ctrl+K is the jump palette, from anywhere in the app — the shortcut
  // means "find" everywhere else, and finding is the thing done most often.
  // Adding is a row in the palette rather than a second chord.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setJumpOpen((open) => !open);
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
          className="flex shrink-0 items-center gap-3 px-2 pb-6 select-none"
          title="Backhog"
          onClick={onLogoClick}
        >
          {mark("lg")}
          <div>
            <p className={cn("text-ink-100", wordmarkClass[family])}>Backhog</p>
            {arena === "books" ? (
              <ReadingSubline
                fallback={bookStats ? `${bookStats.backlog} left to read` : "\u00a0"}
                className={cn("mt-1", sublineClass[family])}
              />
            ) : (
              <p className={cn("mt-1 text-ink-400", sublineClass[family])}>
                {stats ? `${stats.backlog} in the backlog` : "\u00a0"}
              </p>
            )}
          </div>
        </div>

        <ArenaSwitch arena={arena} onSwitch={switchArena} className="mb-4" />

        {/* The palette, dressed as the search box it is. */}
        <button
          type="button"
          onClick={() => setJumpOpen(true)}
          className="mb-3 flex w-full shrink-0 items-center gap-2.5 rounded-xl border border-edge bg-ink-850 px-3 py-2 text-sm text-ink-500 transition-colors hover:border-edge-strong hover:text-ink-300 focus-visible:focus-ring"
        >
          <Gi name="search" className="size-3.5" />
          <span className="flex-1 text-left">{arena === "books" ? "Find a book" : "Find a game"}</span>
          <kbd className="rounded border border-edge px-1.5 py-0.5 font-sans text-[10px] text-ink-500">
            ⌘K
          </kbd>
        </button>

        <Button variant="primary" className="mb-4 w-full shrink-0" onClick={() => openAdd()}>
          <Gi name="plus" className="size-3.5" />
          {arena === "books" ? "Add book" : "Add game"}
        </Button>

        {/* Same anti-deliberation device on both sides of the house — the
            arena only decides which length the picks reason about. */}
        <Button
          variant="secondary"
          className="mb-5 w-full shrink-0"
          onClick={() => (arena === "books" ? setReadOpen(true) : setPickOpen(true))}
        >
          <Gi name="dices" className="size-3.5" />
          {arena === "books" ? "What should I read?" : "What should I play?"}
        </Button>

        <NavScroller>
          <nav className="space-y-1">
            {navItems.map(({ to, label, icon, end }) => (
              <NavLink key={to} to={to} end={end} className={navLinkClass}>
                <Gi name={icon} className="size-4" />
                {label}
              </NavLink>
            ))}
          </nav>

          {smartLists.length > 0 && (
            <div className="mt-6 pb-1">
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
        </NavScroller>

        <div className="mt-3 shrink-0 space-y-1 border-t-2 border-line pt-3">
          {arena === "games" && (
            <button onClick={() => setImportOpen(true)} className={actionLinkClass}>
              <Gi name="download" className="size-4" />
              Import from Steam
            </button>
          )}
          {isAdmin && (
            <NavLink to="/admin" className={navLinkClass}>
              <Gi name="family-tree" className="size-4" />
              <span className="truncate">Accounts</span>
            </NavLink>
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

      {/* Mobile chrome; the sidebar collapses away below lg. The top bar
          holds the actions, the bottom bar holds the places — four of them,
          with labels, and the rest behind More. */}
      <header className="f-panel fixed inset-x-2 top-2 z-30 flex items-center gap-1 px-3 py-1.5 lg:hidden">
        <button type="button" aria-label="Backhog" className="shrink-0" onClick={onLogoClick}>
          {mark("sm")}
        </button>
        <ArenaSwitch arena={arena} onSwitch={switchArena} compact />
        <div className="min-w-0 flex-1" />
        <button
          onClick={() => setJumpOpen(true)}
          className="shrink-0 p-2 text-ink-400 transition-colors hover:text-ink-100"
          aria-label={arena === "books" ? "Find a book" : "Find a game"}
        >
          <Gi name="search" className="size-4" />
        </button>
        <button
          onClick={() => (arena === "books" ? setReadOpen(true) : setPickOpen(true))}
          className="shrink-0 p-2 text-ink-400 transition-colors hover:text-ink-100"
          aria-label={arena === "books" ? "What should I read?" : "What should I play?"}
        >
          <Gi name="dices" className="size-4" />
        </button>
        <Button size="sm" variant="primary" className="shrink-0" onClick={() => openAdd()}>
          <Gi name="plus" className="size-3.5" />
          Add
        </Button>
      </header>

      <nav
        aria-label="Primary"
        className="f-panel fixed inset-x-2 bottom-2 z-30 flex items-stretch pb-[env(safe-area-inset-bottom)] lg:hidden"
      >
        {primaryItems.map(({ to, label, icon, end }) => (
          <NavLink key={to} to={to} end={end} className={tabLinkClass}>
            <Gi name={icon} className="size-5" />
            <span className="truncate">{label.replace(/^(Reading|Play) /, "")}</span>
          </NavLink>
        ))}
        <button
          type="button"
          onClick={() => setMoreOpen(true)}
          aria-haspopup="dialog"
          className={tabLinkClass({ isActive: false })}
        >
          <Gi name="sliders" className="size-5" />
          <span>More</span>
        </button>
      </nav>

      {/* The player bar publishes its own height as --player-h (0 when no
          book is open), so the last row of a page is never buried under it;
          below lg the tab bar's height is padded on top of it. */}
      <main className="min-w-0 flex-1 pb-[calc(var(--player-h,0px)+4.5rem)] pt-20 lg:pb-[var(--player-h,0px)] lg:pl-[17rem] lg:pt-0">
        <ErrorBoundary>
          <Outlet
            context={{
              openAddDialog: () => openAdd(),
              openReadDialog: () => setReadOpen(true),
              openJumpDialog: () => setJumpOpen(true),
            }}
          />
        </ErrorBoundary>
      </main>

      <Dialog open={moreOpen} onClose={() => setMoreOpen(false)} label="More" className="max-w-sm">
        <nav className="space-y-1" onClick={() => setMoreOpen(false)}>
          {moreItems.map(({ to, label, icon, end }) => (
            <NavLink key={to} to={to} end={end} className={navLinkClass}>
              <Gi name={icon} className="size-4" />
              {label}
            </NavLink>
          ))}
          {smartLists.map((list) => (
            <NavLink key={list.id} to={`/lists/${list.id}`} className={navLinkClass}>
              <Gi name="sparkles" className="size-3.5 shrink-0 text-hl-bright" />
              <span className="truncate">{list.name}</span>
              <span className="ml-auto shrink-0 font-display text-[10px] tabular-nums text-ink-400">
                {list.count}
              </span>
            </NavLink>
          ))}
          <div className="my-2 border-t-2 border-line" />
          {arena === "games" && (
            <button onClick={() => setImportOpen(true)} className={actionLinkClass}>
              <Gi name="download" className="size-4" />
              Import from Steam
            </button>
          )}
          {isAdmin && (
            <NavLink to="/admin" className={navLinkClass}>
              <Gi name="family-tree" className="size-4" />
              Accounts
            </NavLink>
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
            className={actionLinkClass}
          >
            <Gi name="log-out" className="size-4" />
            Sign out
          </button>
        </nav>
      </Dialog>

      <JumpDialog open={jumpOpen} onClose={() => setJumpOpen(false)} arena={arena} onAdd={openAdd} />
      <AddGameDialog
        open={addOpen && arena === "games"}
        onClose={() => setAddOpen(false)}
        initialQuery={addQuery}
      />
      <AddBookDialog
        open={addOpen && arena === "books"}
        onClose={() => setAddOpen(false)}
        initialQuery={addQuery}
      />
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
 * The line under the wordmark in the books arena: the book you are in the
 * middle of and how far in, one click from the page you stopped on. More
 * useful chrome for a reader than the size of the pile, which is what it
 * shows when nothing is in progress.
 *
 * Its own component because it resumes through the player, and the player's
 * provider wraps the chrome rather than the Layout component itself.
 */
function ReadingSubline({ fallback, className }: { fallback: string; className: string }) {
  const { data: readingNow } = useReadingNow();
  const continueReading = useContinueReading();
  const current = readingNow?.books[0] ?? null;

  if (!current) return <p className={cn("text-ink-400", className)}>{fallback}</p>;

  return (
    <button
      type="button"
      onClick={(event) => {
        // The wordmark around this counts clicks for an egg; a resume is not one.
        event.stopPropagation();
        continueReading(current.entry);
      }}
      title={`Continue ${current.entry.book.title}`}
      className={cn(
        "flex max-w-[9.5rem] items-baseline gap-1.5 text-left text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring",
        className,
      )}
    >
      <span className="truncate">{current.entry.book.title}</span>
      <span className="shrink-0 tabular-nums text-hl-bright">{Math.round(current.percent)}%</span>
    </button>
  );
}

/**
 * The sidebar's middle: the only part that is allowed to grow.
 *
 * The panel is a fixed-height column, so before this existed every child
 * competed for the same pixels — a short window or a handful of smart lists
 * squeezed the nav, the two big buttons and the wordmark all at once, and the
 * bottom of the list simply fell off the panel. Now the head and the foot
 * hold their size (`shrink-0`) and everything between them scrolls.
 *
 * The edge fade is a mask rather than a gradient overlay, because Midnight's
 * sidebar is translucent over a blurred backdrop: an overlay would have to
 * know what colour to fade to, and would smear the blur. A mask fades the
 * content's own alpha and needs to know nothing about the surface under it,
 * which is what makes one rule work in all three families.
 */
function NavScroller({ children }: { children: React.ReactNode }) {
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ top: false, bottom: false });

  useEffect(() => {
    const el = viewport.current;
    if (!el) return;

    const measure = () => {
      // A pixel of slack: fractional scroll positions and zoomed layouts
      // otherwise leave the bottom fade on forever at the end of the list.
      const scrollable = el.scrollHeight - el.clientHeight;
      const next = { top: el.scrollTop > 1, bottom: scrollable > 1 && el.scrollTop < scrollable - 1 };
      setEdges((prev) => (prev.top === next.top && prev.bottom === next.bottom ? prev : next));
    };

    measure();
    el.addEventListener("scroll", measure, { passive: true });
    // Two things change what is scrollable without a scroll event: the window
    // resizing (the viewport) and the smart lists arriving (the content).
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    if (content.current) observer.observe(content.current);
    return () => {
      el.removeEventListener("scroll", measure);
      observer.disconnect();
    };
  }, []);

  return (
    <div
      ref={viewport}
      // Negative margin against the aside's padding so the scrollbar rides
      // the panel edge instead of cutting through the labels.
      className="scroll-fade -mr-2 min-h-0 flex-1 overflow-y-auto pr-2"
      data-fade-top={edges.top || undefined}
      data-fade-bottom={edges.bottom || undefined}
    >
      <div ref={content}>{children}</div>
    </div>
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

const tabLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "flex min-w-0 flex-1 flex-col items-center gap-1 px-1 py-2 text-[10px] font-medium transition-colors focus-visible:focus-ring",
    isActive ? "text-hl-bright" : "text-ink-400 hover:text-ink-200",
  );
