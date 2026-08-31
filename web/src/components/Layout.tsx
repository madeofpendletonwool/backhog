import { cn } from "@/lib/cn";
import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";

import { AchievementToasts } from "./AchievementToasts";
import { AddGameDialog } from "./AddGameDialog";
import { PickDialog } from "./PickDialog";
import { SteamImportDialog } from "./SteamImportDialog";
import { Button, Gi } from "./ui/primitives";
import { Sprite } from "./ui/Sprite";
import { useAuth } from "@/hooks/useAuth";
import { useEggUnlock } from "@/hooks/useAchievements";
import { useLists } from "@/hooks/useLists";
import { useStats } from "@/hooks/useLibrary";
import type { GiName } from "@/lib/gameicons";

const navItems: { to: string; label: string; icon: GiName; end: boolean }[] = [
  { to: "/", label: "Dashboard", icon: "gauge", end: true },
  { to: "/library", label: "Library", icon: "layout-grid", end: true },
  { to: "/queue", label: "Play Queue", icon: "list-ordered", end: false },
  { to: "/debt", label: "Backlog Debt", icon: "hourglass", end: false },
  { to: "/series", label: "Series", icon: "layers", end: false },
  { to: "/achievements", label: "Achievements", icon: "trophy", end: false },
  { to: "/lists", label: "Lists", icon: "list-tree", end: false },
  { to: "/projects", label: "Projects", icon: "target", end: false },
];

export function Layout() {
  const [addOpen, setAddOpen] = useState(false);
  const [pickOpen, setPickOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const { data: stats } = useStats();
  const { data: listData } = useLists();
  const fireEgg = useEggUnlock();

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

  const smartLists = listData?.lists.filter((list) => list.kind === "smart") ?? [];

  return (
    <div className="flex min-h-screen">
      <aside className="f-panel fixed inset-y-2 left-2 z-20 hidden w-60 flex-col px-3 py-5 lg:flex">
        <div
          className="flex items-center gap-3 px-2 pb-6 select-none"
          title="Backhog"
          onClick={onLogoClick}
        >
          <Sprite name="stick" />
          <div>
            <p className="font-pixel text-[13px] font-bold uppercase tracking-widest text-ink-100">
              Backhog
            </p>
            <p className="mt-1 font-pixel text-[9px] uppercase tracking-wider text-ink-400">
              {stats ? `${stats.backlog} in the backlog` : "\u00a0"}
            </p>
          </div>
        </div>

        <Button variant="primary" className="mb-4 w-full" onClick={() => setAddOpen(true)}>
          <Gi name="plus" className="size-3.5" />
          Add game
          <kbd className="ml-auto rounded-[2px] bg-black/20 px-1.5 py-0.5 font-sans text-[10px] font-normal normal-case tracking-normal text-black/60">
            ⌘K
          </kbd>
        </Button>

        <Button variant="secondary" className="mb-6 w-full" onClick={() => setPickOpen(true)}>
          <Gi name="dices" className="size-3.5" />
          What should I play?
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
            <p className="px-2 pb-2 font-pixel text-[9px] font-bold uppercase tracking-widest text-ink-400">
              Smart lists
            </p>
            <div className="space-y-1">
              {smartLists.map((list) => (
                <NavLink key={list.id} to={`/lists/${list.id}`} className={navLinkClass}>
                  <Gi name="sparkles" className="size-3.5 shrink-0 text-gold-bright" />
                  <span className="truncate">{list.name}</span>
                  <span className="ml-auto shrink-0 font-pixel text-[10px] tabular-nums text-ink-400">
                    {list.count}
                  </span>
                </NavLink>
              ))}
            </div>
          </div>
        )}

        <div className="mt-auto space-y-1 border-t-2 border-line pt-3">
          <button onClick={() => setImportOpen(true)} className={actionLinkClass}>
            <Gi name="download" className="size-4" />
            Import from Steam
          </button>
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
          <Sprite name="ball" className="h-8 w-8" />
        </button>
        <nav className="flex flex-1 items-center gap-1 overflow-x-auto">
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
          onClick={() => setPickOpen(true)}
          className="p-2 text-ink-400 transition-colors hover:text-ink-100"
          aria-label="What should I play?"
        >
          <Gi name="dices" className="size-4" />
        </button>
        <Button size="sm" variant="primary" onClick={() => setAddOpen(true)}>
          <Gi name="plus" className="size-3.5" />
          Add
        </Button>
      </header>

      <main className="min-w-0 flex-1 pt-20 lg:pl-[17rem] lg:pt-0">
        <Outlet context={{ openAddDialog: () => setAddOpen(true) }} />
      </main>

      <AddGameDialog open={addOpen} onClose={() => setAddOpen(false)} />
      <PickDialog open={pickOpen} onClose={() => setPickOpen(false)} />
      <SteamImportDialog open={importOpen} onClose={() => setImportOpen(false)} />
      <AchievementToasts />
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
  cn("p-2 transition-colors", isActive ? "text-gold-bright" : "text-ink-400");
