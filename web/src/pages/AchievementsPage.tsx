import { Link } from "react-router-dom";

import { GameCover } from "@/components/GameCover";
import { achievementIcon } from "@/components/achievementIcons";
import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useAchievements } from "@/hooks/useAchievements";
import { formatDate } from "@/lib/format";
import { ACHIEVEMENT_TIERS, type AchievementStatus, type AchievementTier } from "@/lib/types";
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
 * The trophy wall: every achievement in the catalogue with its unlock state,
 * grouped by tier. Locked cards show what's still owed; unlocked ones carry
 * the date and the game that tipped them over. Hidden achievements arrive
 * masked from the API and reveal on unlock.
 */
export function AchievementsPage() {
  const { data, isLoading } = useAchievements();

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

  const achievements = data?.achievements ?? [];
  const unlocked = achievements.filter((a) => a.unlocked_at).length;

  if (achievements.length === 0) {
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
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Achievements</h1>
        <p className="mt-1 text-sm text-ink-400">
          Progress through the backlog, not hours in it.{" "}
          <span className="font-medium text-ink-200 tabular-nums">
            {unlocked} of {achievements.length} earned
          </span>
        </p>
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

      {unlockedAt && achievement.entry && (
        <div className="mt-3 flex items-center gap-3 border-t border-white/[0.06] pt-3">
          <Link
            to={`/game/${achievement.entry.id}`}
            className="w-10 shrink-0 overflow-hidden rounded-lg ring-1 ring-white/[0.08] transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 focus-visible:focus-ring"
            aria-label={achievement.entry.game.name}
          >
            <GameCover game={achievement.entry.game} sizes="40px" />
          </Link>
          <Link
            to={`/game/${achievement.entry.id}`}
            className="min-w-0 truncate text-sm font-medium text-ink-200 hover:text-white focus-visible:focus-ring"
          >
            {achievement.entry.game.name}
          </Link>
        </div>
      )}
    </Panel>
  );
}
