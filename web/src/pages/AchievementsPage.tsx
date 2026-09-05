import { Link } from "react-router-dom";
import { useEffect, useRef } from "react";

import { BookCover } from "@/components/BookCover";
import { useMediaFilter } from "@/components/MediaFilter";
import { GameCover } from "@/components/GameCover";
import { achievementIcon } from "@/components/achievementIcons";
import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useAchievements, useEggUnlock } from "@/hooks/useAchievements";
import type { MediaFilterValue } from "@/lib/entry";
import { formatDate } from "@/lib/format";
import { isBookEntry } from "@/lib/types";
import { ACHIEVEMENT_TIERS, type AchievementStatus, type AchievementTier } from "@/lib/types";
import { cn } from "@/lib/cn";

/** The medal each tier section is headlined with. */
const TIER_META: Record<AchievementTier, { label: string; emoji: string }> = {
  bronze: { label: "Bronze", emoji: "🥉" },
  silver: { label: "Silver", emoji: "🥈" },
  gold: { label: "Gold", emoji: "🥇" },
  legendary: { label: "Legendary", emoji: "💎" },
};

/* Unlocked icon chips wear their tier; locked ones stay quiet gray.

   The medal tones are pale by design — they were picked to sit against
   near-black. Used as `text-*` on a light ground they fail badly (#82e6ff
   legendary is about 1.4:1 on cream), so the tier is handed over as --tone
   and the ink is mixed toward the theme's own strongest ink. Same treatment
   as the status badges; see the `tone-chip` utility in index.css. */
const TIER_TONE: Record<AchievementTier, string> = {
  bronze: "var(--color-tier-bronze)",
  silver: "var(--color-tier-silver)",
  gold: "var(--color-tier-gold)",
  legendary: "var(--color-tier-legendary)",
};

/* The medal tones *are* the ink on a dark ground — they were picked that
   way — so tone and ink are the same value here. Only Paper moves them. */
const tierStyle = (tier: AchievementTier) =>
  ({ "--tone": TIER_TONE[tier], "--tone-ink": TIER_TONE[tier] }) as React.CSSProperties;

/**
 * The gallery's domain tabs. "any" achievements (the eggs) are about the app
 * itself, so they answer every tab.
 *
 * The keys are the media filter's own values, because the tab lives in the
 * URL like every other cross-arena filter: that is what lets the books nav
 * point at ?media=book and land on the books half of a wall both arenas
 * share, without a second copy of this page under /books.
 */
const DOMAIN_TABS: { key: MediaFilterValue; label: string }[] = [
  { key: "all", label: "All" },
  { key: "game", label: "Games" },
  { key: "book", label: "Books" },
];

function inDomain(a: AchievementStatus, tab: MediaFilterValue): boolean {
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
  // "all" is the fallback: arriving from the games nav, or from a bare
  // /achievements link, still opens on the whole wall the way it always has.
  const [tab, setTab] = useMediaFilter("all");

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
          <div className="flex gap-1 rounded-xl bg-ink-900 p-1 ring-1 ring-edge">
            {DOMAIN_TABS.map(({ key, label }) => (
              <button
                key={key}
                type="button"
                onClick={() => setTab(key)}
                className={cn(
                  "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors focus-visible:focus-ring",
                  tab === key
                    ? "bg-fill-active text-ink-100"
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
                <h2
                  style={tierStyle(tier)}
                  className="tone-ink text-sm font-semibold uppercase tracking-wider"
                >
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
      <div className="mt-3 flex items-center gap-3 border-t border-edge pt-3">
        <Link
          to={`/books/${entry.id}`}
          className="w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-art transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 focus-visible:focus-ring"
          aria-label={entry.book.title}
        >
          <BookCover book={entry.book} sizes="40px" />
        </Link>
        <Link
          to={`/books/${entry.id}`}
          className="min-w-0 truncate text-sm font-medium text-ink-200 hover:text-ink-max focus-visible:focus-ring"
        >
          {entry.book.title}
        </Link>
      </div>
    );
  }

  return (
    <div className="mt-3 flex items-center gap-3 border-t border-edge pt-3">
      <Link
        to={`/game/${entry.id}`}
        className="w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-art transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 focus-visible:focus-ring"
        aria-label={entry.game.name}
      >
        <GameCover game={entry.game} sizes="40px" />
      </Link>
      <Link
        to={`/game/${entry.id}`}
        className="min-w-0 truncate text-sm font-medium text-ink-200 hover:text-ink-max focus-visible:focus-ring"
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
          style={unlockedAt ? tierStyle(achievement.tier) : undefined}
          className={cn(
            "flex size-10 items-center justify-center rounded-xl",
            // tone-chip draws its own ring; the locked tile needs one.
            unlockedAt ? "tone-chip" : "bg-ink-850 text-ink-600 ring-1 ring-edge",
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
