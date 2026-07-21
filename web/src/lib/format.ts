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

/** The full release date ("Feb 25, 2022"), for the detail page. */
export function releaseDate(game: Game): string {
  if (!game.first_release_date) return "";
  return new Date(game.first_release_date * 1000).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

// IGDB's website `category` enum is being deprecated and now comes back empty,
// so we label each link from its URL instead — which also relabels links whose
// metadata was cached before this existed.
const WEBSITE_RULES: [RegExp, string][] = [
  [/nintendo/, "Nintendo"],
  [/playstation/, "PlayStation Store"],
  [/steampowered|steamcommunity/, "Steam"],
  [/xbox/, "Xbox"],
  [/epicgames/, "Epic Games"],
  [/gog\.com/, "GOG"],
  [/microsoft/, "Microsoft Store"],
  [/apps\.apple|itunes\.apple/, "App Store"],
  [/play\.google/, "Google Play"],
  [/twitch/, "Twitch"],
  [/youtube|youtu\.be/, "YouTube"],
  [/twitter|(^|\.)x\.com/, "Twitter / X"],
  [/facebook/, "Facebook"],
  [/instagram/, "Instagram"],
  [/discord/, "Discord"],
  [/reddit/, "Reddit"],
  [/wikipedia/, "Wikipedia"],
  [/fandom|wikia/, "Wiki"],
  [/itch\.io/, "itch.io"],
];

/**
 * A human label for an external link, derived from its host. Known storefronts
 * and socials get a proper name; anything else falls back to the bare domain
 * (e.g. "eldenring.com") so links stay distinguishable instead of all reading
 * "Website".
 */
export function websiteLabel(url: string): string {
  try {
    const host = new URL(url).hostname.replace(/^www\./, "").toLowerCase();
    for (const [pattern, label] of WEBSITE_RULES) {
      if (pattern.test(host)) return label;
    }
    return host || "Website";
  } catch {
    return "Website";
  }
}

const IGDB_IMG = "https://images.igdb.com/igdb/image/upload";

/** Builds an IGDB CDN URL for an image id at a named size preset. */
export const igdbImage = (imageId: string, size: string) => `${IGDB_IMG}/t_${size}/${imageId}.jpg`;

/** Screenshot presets: a medium thumbnail that links to the full-size image. */
export const screenshotThumbUrl = (imageId: string) => igdbImage(imageId, "screenshot_med");
export const screenshotUrl = (imageId: string) => igdbImage(imageId, "screenshot_huge");

/** A small cover for related-game thumbnails (similar / DLC / expansions). */
export const relatedCoverUrl = (imageId: string) => igdbImage(imageId, "cover_small_2x");

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
