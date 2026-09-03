import { Link } from "react-router-dom";
import { useEffect, useRef, useState } from "react";

import { BookCover } from "@/components/BookCover";
import { GameCover } from "@/components/GameCover";
import { achievementIcon } from "@/components/achievementIcons";
import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useAchievements, useEggUnlock } from "@/hooks/useAchievements";
import { formatDate } from "@/lib/format";
import { isBookEntry } from "@/lib/types";
import { ACHIEVEMENT_TIERS, type AchievementDomain, type AchievementStatus, type AchievementTier } from "@/lib/types";
import { cn } from "@/lib/cn";

/** The medal each tier section is headlined with. */
const TIER_META: Record<AchievementTier, { label: string; emoji: string }> = {
  bronze: { label: "Bronze", emoji: "🥉" },
  silver: { label: "Silver", emoji: "🥈" },
  gold: { label: "Gold", emoji: "🥇" },
  legendary: { label: "Legendary", emoji: "💎" },
};

/** Unlocked icon chips wear their tier; locked ones stay quiet gray. */
const TIER_CHIP: Record<AchievementTier, string> = {
  bronze: "bg-tier-bronze/10 text-tier-bronze ring-tier-bronze/30",
  silver: "bg-tier-silver/10 text-tier-silver ring-tier-silver/30",
  gold: "bg-tier-gold/10 text-tier-gold ring-tier-gold/30",
  legendary: "bg-tier-legendary/10 text-tier-legendary ring-tier-legendary/30",
};

const TIER_TEXT: Record<AchievementTier, string> = {
  bronze: "text-tier-bronze",
  silver: "text-tier-silver",
  gold: "text-tier-gold",
  legendary: "text-tier-legendary",
};

/**
 * The gallery's domain tabs. "any" achievements (the eggs) are about the app
 * itself, so they answer every tab.
 */
const DOMAIN_TABS: { key: AchievementDomain | "all"; label: string }[] = [
  { key: "all", label: "All" },
  { key: "game", label: "Games" },
  { key: "book", label: "Books" },
];

function inDomain(a: AchievementStatus, tab: AchievementDomain | "all"): boolean {
  return tab === "all" || a.domain === "any" || a.domain === tab;
}

/**
 * The trophy wall: every achievement in the catalogue with its unlock state,
 * grouped by arena and tier. Locked cards show what's still owed; unlocked
 * ones carry the date and the entry that tipped them over. Hidden
 * achievements arrive masked from the API and reveal on unlock.
 */
/** The Konami code, as key events read it. */
const KONAMI = ["arrowup", "arrowup", "arrowdown", "arrowdown",
  "arrowleft", "arrowright", "arrowleft", "arrowright", "b", "a"];

export function AchievementsPage() {
  const { data, isLoading } = useAchievements();
  const fireEgg = useEggUnlock();
  const [tab, setTab] = useState<AchievementDomain | "all">("all");

  // Old habits: typing the Konami code anywhere on the gallery hatches
  // the Old Habits egg. Progress resets on any wrong key — as it should.
  const konamiPos = useRef(0);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const key = event.key.toLowerCase();
      if (key === KONAMI[konamiPos.current]) {
        konamiPos.current += 1;
      } else {
        // A wrong key restarts the watch — unless it is itself the
        // opening move, in which case the run starts there.
        konamiPos.current = key === KONAMI[0] ? 1 : 0;
      }
      if (konamiPos.current === KONAMI.length) {
        konamiPos.current = 0;
        void fireEgg("konami");
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [fireEgg]);

  if (isLoading) {
    return (
      <div className="mx-auto max-w-5xl space-y-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <Skeleton className="h-16" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className="h-40" />
          ))}
        </div>
      </div>
    );
  }

  const all = data?.achievements ?? [];
  const achievements = all.filter((a) => inDomain(a, tab));
  const unlocked = achievements.filter((a) => a.unlocked_at).length;

  if (all.length === 0) {
    return (
      <EmptyState
        icon={<Gi name="trophy" className="size-7" />}
        title="No achievements yet"
        description="The catalogue is empty — that shouldn't happen."
      />
    );
  }

  const tierOf = (a: AchievementStatus): AchievementTier =>
    ACHIEVEMENT_TIERS.includes(a.tier) ? a.tier : "bronze";

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Achievements</h1>
            <p className="mt-1 text-sm text-ink-400">
              Progress through the pile, not hours in it.{" "}
              <span className="font-medium text-ink-200 tabular-nums">
                {unlocked} of {achievements.length} earned
              </span>
            </p>
          </div>
          <div className="flex gap-1 rounded-xl bg-ink-900 p-1 ring-1 ring-white/[0.06]">
            {DOMAIN_TABS.map(({ key, label }) => (
              <button
                key={key}
                type="button"
                onClick={() => setTab(key)}
                className={cn(
                  "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors focus-visible:focus-ring",
                  tab === key
                    ? "bg-white/[0.08] text-ink-100"
                    : "text-ink-500 hover:text-ink-300",
                )}
                aria-pressed={tab === key}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
      </header>

      <div className="space-y-8">
        {ACHIEVEMENT_TIERS.map((tier) => {
          const group = achievements.filter((a) => tierOf(a) === tier);
          if (group.length === 0) return null;
          const earned = group.filter((a) => a.unlocked_at).length;
          return (
            <section key={tier}>
              <div className="mb-3 flex items-baseline gap-2">
                <span className="text-base leading-none" aria-hidden>
                  {TIER_META[tier].emoji}
                </span>
                <h2 className={cn("text-sm font-semibold uppercase tracking-wider", TIER_TEXT[tier])}>
                  {TIER_META[tier].label}
                </h2>
                <span className="text-xs text-ink-500 tabular-nums">
                  {earned} of {group.length}
                </span>
              </div>
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                {group.map((achievement) => (
                  <AchievementCard key={achievement.id} achievement={achievement} />
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}

/** The triggering entry, linked and covered, whichever arena it came from. */
function AchievementEntryLink({ achievement }: { achievement: AchievementStatus }) {
  const entry = achievement.entry;
  if (!entry) return null;

  if (isBookEntry(entry)) {
    return (
      <div className="mt-3 flex items-center gap-3 border-t border-white/[0.06] pt-3">
        <Link
          to={`/books/${entry.id}`}
          className="w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-white/[0.08] transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 focus-visible:focus-ring"
          aria-label={entry.book.title}
        >
          <BookCover book={entry.book} sizes="40px" />
        </Link>
        <Link
          to={`/books/${entry.id}`}
          className="min-w-0 truncate text-sm font-medium text-ink-200 hover:text-white focus-visible:focus-ring"
        >
          {entry.book.title}
        </Link>
      </div>
    );
  }

  return (
    <div className="mt-3 flex items-center gap-3 border-t border-white/[0.06] pt-3">
      <Link
        to={`/game/${entry.id}`}
        className="w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-white/[0.08] transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 focus-visible:focus-ring"
        aria-label={entry.game.name}
      >
        <GameCover game={entry.game} sizes="40px" />
      </Link>
      <Link
        to={`/game/${entry.id}`}
        className="min-w-0 truncate text-sm font-medium text-ink-200 hover:text-white focus-visible:focus-ring"
      >
        {entry.game.name}
      </Link>
    </div>
  );
}

function AchievementCard({ achievement }: { achievement: AchievementStatus }) {
  const unlockedAt = achievement.unlocked_at;

  return (
    <Panel className={`animate-fade-rise p-4 ${unlockedAt ? "" : "opacity-60"}`}>
      <div className="flex items-start justify-between gap-3">
        <div
          className={cn(
            "flex size-10 items-center justify-center rounded-xl ring-1",
            unlockedAt ? TIER_CHIP[achievement.tier] : "bg-ink-850 text-ink-600 ring-white/[0.06]",
          )}
        >
          <Gi name={achievementIcon(achievement.icon)} className="size-5" />
        </div>
        {unlockedAt ? (
          <span className="text-[11px] font-medium text-ink-500">{formatDate(unlockedAt)}</span>
        ) : (
          <Gi name="lock" className="size-4 text-ink-600" />
        )}
      </div>

      <p className="mt-3 text-[15px] font-semibold tracking-tight text-ink-100">
        {achievement.title}
      </p>
      <p className="mt-1 text-sm leading-relaxed text-ink-400">{achievement.description}</p>

      {unlockedAt && <AchievementEntryLink achievement={achievement} />}
    </Panel>
  );
}
