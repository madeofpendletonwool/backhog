import type { Book, BookEdition, Game } from "./types";

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

/**
 * A span of weeks as a human sentence: "1 year 6 months", "8 months",
 * "3 weeks". Used for backlog clearance estimates, where decimal weeks
 * would be noise.
 */
export function formatTimespan(weeks: number): string {
  if (weeks <= 0) return "no time at all";
  const totalDays = weeks * 7;
  if (totalDays < 60) {
    const w = Math.max(1, Math.round(weeks));
    return `${w} week${w === 1 ? "" : "s"}`;
  }
  // Average month/year lengths keep the rounding stable across year spans.
  const totalMonths = totalDays / 30.44;
  const years = Math.floor(totalMonths / 12);
  const months = Math.round(totalMonths - years * 12);
  if (months === 12) return plural(years + 1, "year");
  if (years === 0) return plural(Math.max(months, 1), "month");
  if (months === 0) return plural(years, "year");
  return `${plural(years, "year")} ${plural(months, "month")}`;
}

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? "" : "s"}`;
}

/** "March 2027" from a plain 2027-03-05 date, in local time. */
export function formatMonthYear(date: string | null): string {
  if (!date) return "never";
  return new Date(`${date}T00:00:00`).toLocaleDateString(undefined, {
    year: "numeric",
    month: "long",
  });
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
 * the brand purple when the subject has no cover to sample. Games and books
 * both carry one, sampled server-side from their own artwork.
 */
export function accentStyle(subject: { accent_hex: string }): React.CSSProperties {
  const accent = subject.accent_hex || "#8b5cf6";
  return { ["--accent" as string]: accent };
}

/** "Ursula K. Le Guin", "Gaiman & Pratchett", "" — the byline, one line long. */
export function byline(book: Book): string {
  const authors = book.authors ?? [];
  if (authors.length === 0) return "";
  if (authors.length === 1) return authors[0];
  if (authors.length === 2) return `${authors[0]} & ${authors[1]}`;
  return `${authors[0]} & ${authors.length - 1} others`;
}

/** The year the work first appeared, as a string; "" when unknown. */
export function publishYear(book: Book): string {
  return book.first_publish_year ? String(book.first_publish_year) : "";
}

/** "384 pages" / "" — page counts belong to a printing, not to the work. */
export function formatPages(pages: number | null | undefined): string {
  if (!pages || pages <= 0) return "";
  return `${pages.toLocaleString()} page${pages === 1 ? "" : "s"}`;
}

/**
 * A one-line description of a printing for the edition picker: publisher,
 * year, binding and page count, with the empty parts dropped rather than
 * rendered as dashes.
 */
export function editionLabel(edition: BookEdition): string {
  return [
    edition.publisher,
    edition.published_year ? String(edition.published_year) : "",
    edition.binding,
    formatPages(edition.page_count),
  ]
    .filter(Boolean)
    .join(" · ");
}

/** The ISBN to show for a printing; ISBN-13 wins when both are known. */
export function editionISBN(edition: BookEdition): string {
  return edition.isbn13 || edition.isbn10 || "";
}

/**
 * Strips the separators people type or scanners emit, so "978-0-14-118776-1"
 * and "9780141187761" are the same lookup. Validation is the API's job — this
 * only normalises what gets sent.
 */
export function normalizeISBN(raw: string): string {
  return raw.replace(/[\s-]/g, "").toUpperCase();
}

/** ISBN-10 (last digit may be X) or ISBN-13, once separators are gone. */
export function looksLikeISBN(raw: string): boolean {
  const isbn = normalizeISBN(raw);
  return /^\d{9}[\dX]$/.test(isbn) || /^\d{13}$/.test(isbn);
}
