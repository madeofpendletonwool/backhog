import { Lock, Trophy } from "lucide-react";
import { Link } from "react-router-dom";

import { GameCover } from "@/components/GameCover";
import { achievementIcon } from "@/components/achievementIcons";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useAchievements } from "@/hooks/useAchievements";
import { formatDate } from "@/lib/format";
import type { AchievementStatus } from "@/lib/types";

/**
 * The trophy wall: every achievement in the catalogue with its unlock state.
 * Locked cards show what's still owed; unlocked ones carry the date and the
 * game that tipped them over.
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
        icon={<Trophy className="size-7" />}
        title="No achievements yet"
        description="The catalogue is empty — that shouldn't happen."
      />
    );
  }

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

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {achievements.map((achievement) => (
          <AchievementCard key={achievement.id} achievement={achievement} />
        ))}
      </div>
    </div>
  );
}

function AchievementCard({ achievement }: { achievement: AchievementStatus }) {
  const Icon = achievementIcon(achievement.icon);
  const unlockedAt = achievement.unlocked_at;

  return (
    <Panel className={`animate-fade-rise p-4 ${unlockedAt ? "" : "opacity-60"}`}>
      <div className="flex items-start justify-between gap-3">
        <div
          className={`flex size-10 items-center justify-center rounded-xl ring-1 ${
            unlockedAt
              ? "bg-brand-600/20 text-brand-300 ring-brand-400/30"
              : "bg-ink-850 text-ink-600 ring-white/[0.06]"
          }`}
        >
          <Icon className="size-5" />
        </div>
        {unlockedAt ? (
          <span className="text-[11px] font-medium text-ink-500">{formatDate(unlockedAt)}</span>
        ) : (
          <Lock className="size-4 text-ink-600" />
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
