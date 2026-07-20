import type { Game } from "./types";

/** Formats a seconds duration as a compact "12h" / "1h 30m" / "45m". */
export function formatDuration(seconds: number | null | undefined): string {
  if (!seconds || seconds <= 0) return "—";
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.round((seconds % 3600) / 60);
  if (hours === 0) return `${minutes}m`;
  if (minutes === 0) return `${hours}h`;
  return `${hours}h ${minutes}m`;
}

/** Rounds a seconds duration to whole hours, for totals. */
export function toHours(seconds: number | null | undefined): number {
  return seconds && seconds > 0 ? seconds / 3600 : 0;
}

export function formatHours(hours: number): string {
  if (hours <= 0) return "0h";
  if (hours < 10) return `${hours.toFixed(1)}h`;
  return `${Math.round(hours)}h`;
}

/** IGDB stores release dates as a unix timestamp in seconds. */
export function releaseYear(game: Game): string {
  if (!game.first_release_date) return "";
  return String(new Date(game.first_release_date * 1000).getUTCFullYear());
}

export function formatDate(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/** "3 days ago", "2 months ago" — used for stalled/aging hints. */
export function relativeTime(iso: string | null): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  const days = Math.floor((Date.now() - then) / 86_400_000);
  if (days < 1) return "today";
  if (days === 1) return "yesterday";
  if (days < 30) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months} month${months === 1 ? "" : "s"} ago`;
  const years = Math.floor(months / 12);
  return `${years} year${years === 1 ? "" : "s"} ago`;
}

/**
 * Builds a translucent accent from the cover's sampled colour, falling back to
 * the brand purple when a game has no cover to sample.
 */
export function accentStyle(game: Game): React.CSSProperties {
  const accent = game.accent_hex || "#8b5cf6";
  return { ["--accent" as string]: accent };
}
