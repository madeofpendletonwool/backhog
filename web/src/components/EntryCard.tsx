import { isBookEntry, type Entry } from "@/lib/types";
import { BookCard } from "./BookCard";
import { GameCard } from "./GameCard";

/**
 * The cover card for an entry of either media, for the shared grids — list
 * detail and a rule goal's match pool. Each arena's own library keeps calling
 * its own card directly.
 */
export function EntryCard({ entry }: { entry: Entry }) {
  return isBookEntry(entry) ? <BookCard entry={entry} /> : <GameCard entry={entry} />;
}
