import { byline, publishYear, releaseYear, toHours } from "./format";
import { isBookEntry, type Entry, type MediaType } from "./types";

/**
 * The handful of facts the *shared* surfaces need from an entry without
 * caring which arena it belongs to: the queue, list detail and project
 * detail all hold games and books in one ordered collection, so they read
 * an entry through here rather than reaching for `.game` and filtering the
 * other half away.
 *
 * Anything a single arena cares about — time to beat, page count, platform —
 * stays behind its own narrowing guard on its own page.
 */

/** The title to show: the game's name, or the book's title. */
export function entryTitle(entry: Entry): string {
  return isBookEntry(entry) ? entry.book.title : entry.game.name;
}

/** The line under the title: authors for a book, release year for a game. */
export function entrySubtitle(entry: Entry): string {
  return isBookEntry(entry)
    ? [byline(entry.book), publishYear(entry.book)].filter(Boolean).join(" · ")
    : releaseYear(entry.game);
}

/** Where this entry's detail page lives. Each arena keeps its own prefix. */
export function entryHref(entry: Entry): string {
  return isBookEntry(entry) ? `/books/${entry.id}` : `/game/${entry.id}`;
}

/** The cover accent, sampled from whichever artwork this entry has. */
export function entryAccent(entry: Entry): { accent_hex: string } {
  return isBookEntry(entry) ? entry.book : entry.game;
}

/**
 * Estimated hours left in this entry. Only games carry a time-to-beat, so a
 * book contributes nothing to a queue's hour total rather than a guess — the
 * reading-time estimate is a page-map feature, and the page map isn't built.
 */
export function entryHours(entry: Entry): number {
  return isBookEntry(entry) ? 0 : toHours(entry.game.time_to_beat_main);
}

/** The media filter shared by the queue, list detail and project detail. */
export type MediaFilterValue = MediaType | "all";

export function matchesMedia(entry: Entry, filter: MediaFilterValue): boolean {
  return filter === "all" || entry.media_type === filter;
}
