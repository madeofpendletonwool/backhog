import { cn } from "@/lib/cn";
import {
  ListOrdered,
  LayoutGrid,
  ListTree,
  LogOut,
  Plus,
  Settings,
  Sparkles,
} from "lucide-react";
import { useEffect, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";

import { AddGameDialog } from "./AddGameDialog";
import { Button } from "./ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { useLists } from "@/hooks/useLists";
import { useStats } from "@/hooks/useLibrary";

const navItems = [
  { to: "/", label: "Library", icon: LayoutGrid, end: true },
  { to: "/queue", label: "Play Queue", icon: ListOrdered, end: false },
  { to: "/lists", label: "Lists", icon: ListTree, end: false },
];

export function Layout() {
  const [addOpen, setAddOpen] = useState(false);
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const { data: stats } = useStats();
  const { data: listData } = useLists();

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
      <aside className="fixed inset-y-0 left-0 hidden w-60 flex-col border-r border-white/[0.06] bg-ink-900/50 px-3 py-5 backdrop-blur-xl lg:flex">
        <div className="flex items-center gap-2.5 px-2 pb-6">
          <span className="text-2xl leading-none">🐗</span>
          <div>
            <p className="text-[15px] font-semibold tracking-tight text-ink-100">Backhog</p>
            <p className="text-[11px] text-ink-500">
              {stats ? `${stats.backlog} in the backlog` : " "}
            </p>
          </div>
        </div>

        <Button variant="primary" className="mb-6 w-full" onClick={() => setAddOpen(true)}>
          <Plus className="size-4" />
          Add game
          <kbd className="ml-auto rounded border border-white/20 px-1.5 py-0.5 font-sans text-[10px] text-white/70">
            ⌘K
          </kbd>
        </Button>

        <nav className="space-y-0.5">
          {navItems.map(({ to, label, icon: Icon, end }) => (
            <NavLink key={to} to={to} end={end} className={navLinkClass}>
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>

        {smartLists.length > 0 && (
          <div className="mt-7">
            <p className="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-ink-500">
              Smart lists
            </p>
            <div className="space-y-0.5">
              {smartLists.map((list) => (
                <NavLink key={list.id} to={`/lists/${list.id}`} className={navLinkClass}>
                  <Sparkles className="size-3.5 shrink-0 text-brand-400" />
                  <span className="truncate">{list.name}</span>
                  <span className="ml-auto shrink-0 text-[11px] tabular-nums text-ink-500">
                    {list.count}
                  </span>
                </NavLink>
              ))}
            </div>
          </div>
        )}

        <div className="mt-auto space-y-0.5 border-t border-white/[0.06] pt-3">
          <NavLink to="/settings" className={navLinkClass}>
            <Settings className="size-4" />
            <span className="truncate">{user?.username}</span>
          </NavLink>
          <button
            onClick={async () => {
              await logout();
              navigate("/login");
            }}
            className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm text-ink-400 transition-colors hover:bg-white/[0.05] hover:text-ink-100 focus-visible:focus-ring"
          >
            <LogOut className="size-4" />
            Sign out
          </button>
        </div>
      </aside>

      {/* Mobile top bar; the sidebar collapses away below lg. */}
      <header className="fixed inset-x-0 top-0 z-30 flex items-center gap-2 border-b border-white/[0.06] bg-ink-950/85 px-4 py-2.5 backdrop-blur-xl lg:hidden">
        <span className="text-xl">🐗</span>
        <nav className="flex flex-1 items-center gap-1 overflow-x-auto">
          {navItems.map(({ to, label, icon: Icon, end }) => (
            <NavLink key={to} to={to} end={end} className={mobileLinkClass} title={label}>
              <Icon className="size-4" />
            </NavLink>
          ))}
          <NavLink to="/settings" className={mobileLinkClass} title="Settings">
            <Settings className="size-4" />
          </NavLink>
        </nav>
        <Button size="sm" variant="primary" onClick={() => setAddOpen(true)}>
          <Plus className="size-4" />
          Add
        </Button>
      </header>

      <main className="min-w-0 flex-1 pt-16 lg:pl-60 lg:pt-0">
        <Outlet context={{ openAddDialog: () => setAddOpen(true) }} />
      </main>

      <AddGameDialog open={addOpen} onClose={() => setAddOpen(false)} />
    </div>
  );
}

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "flex items-center gap-2.5 rounded-xl px-3 py-2 text-sm transition-colors focus-visible:focus-ring",
    isActive
      ? "bg-white/[0.08] font-medium text-ink-100"
      : "text-ink-400 hover:bg-white/[0.05] hover:text-ink-200",
  );

const mobileLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    "rounded-lg p-2 transition-colors",
    isActive ? "bg-white/[0.08] text-ink-100" : "text-ink-400",
  );
