import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";

import { GameCover } from "@/components/GameCover";
import { achievementIcon } from "@/components/achievementIcons";
import { Gi } from "@/components/ui/Gi";
import { UNLOCK_EVENT } from "@/hooks/useAchievements";
import type { AchievementStatus } from "@/lib/types";

interface Toast extends AchievementStatus {
  key: number;
}

const TOAST_MS = 6500;

/**
 * The achievement toast stack. Mutations return their newly unlocked
 * achievements; the hooks broadcast them (see useAchievements) and this
 * component — mounted once in the Layout — celebrates.
 */
export function AchievementToasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextKey = useRef(0);

  useEffect(() => {
    const onUnlock = (event: Event) => {
      const unlocks = (event as CustomEvent<AchievementStatus[]>).detail;
      setToasts((current) => [
        ...current,
        ...unlocks.map((unlock) => ({ ...unlock, key: nextKey.current++ })),
      ]);
    };
    window.addEventListener(UNLOCK_EVENT, onUnlock);
    return () => window.removeEventListener(UNLOCK_EVENT, onUnlock);
  }, []);

  useEffect(() => {
    if (toasts.length === 0) return;
    const timer = setTimeout(() => setToasts((current) => current.slice(1)), TOAST_MS);
    return () => clearTimeout(timer);
  }, [toasts]);

  if (toasts.length === 0) return null;

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 px-4 pb-5 sm:items-end sm:px-6">
      {toasts.map((toast) => (
        <ToastCard key={toast.key} toast={toast} />
      ))}
    </div>
  );
}

function ToastCard({ toast }: { toast: Toast }) {
  return (
    <div className="f-panel animate-fade-rise pointer-events-auto flex w-full max-w-sm items-center gap-3 p-3">
      <div className="f-chip flex size-12 shrink-0 items-center justify-center text-hl-bright">
        <Gi name={achievementIcon(toast.icon)} className="size-5" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-1.5 font-display text-[10px] font-bold uppercase tracking-wider text-hl-bright">
          <Gi name="trophy" className="size-3" />
          Achievement unlocked
        </p>
        <p className="mt-0.5 truncate text-sm font-semibold text-ink-100">{toast.title}</p>
        <p className="truncate text-xs text-ink-400">{toast.description}</p>
      </div>
      {toast.entry && (
        <Link
          to={`/game/${toast.entry.id}`}
          className="w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-white/[0.08] transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 focus-visible:focus-ring"
          aria-label={toast.entry.game.name}
        >
          <GameCover game={toast.entry.game} sizes="40px" />
        </Link>
      )}
    </div>
  );
}
