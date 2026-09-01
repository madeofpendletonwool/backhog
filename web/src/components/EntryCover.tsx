import { isBookEntry, type Entry } from "@/lib/types";
import { BookCover } from "./BookCover";
import { GameCover } from "./GameCover";

/**
 * The cover for an entry of either media, for the surfaces that hold both —
 * the queue, list detail, project detail. Each arena's own pages keep calling
 * GameCover / BookCover directly; this exists so a shared row doesn't have to
 * branch inline every time it wants a thumbnail.
 */
export function EntryCover({
  entry,
  className,
  sizes,
}: {
  entry: Entry;
  className?: string;
  sizes?: string;
}) {
  return isBookEntry(entry) ? (
    <BookCover book={entry.book} className={className} sizes={sizes} />
  ) : (
    <GameCover game={entry.game} className={className} sizes={sizes} />
  );
}
