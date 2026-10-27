import { editionISBN, formatPages, normalizeISBN } from "@/lib/format";
import type { BookEdition } from "@/lib/types";

/**
 * Finding your printing among a popular book's three hundred.
 *
 * The ISBN is the one fact on the copy in your hand that names its printing
 * outright, so it is what the search leans on: digits typed with or without
 * hyphens, either form of the ISBN, any run of it — the last five digits off
 * the back cover are usually enough. Words narrow by publisher, binding and
 * language; a bare number also matches a year or a page count, because
 * "1999" and "480" are what the copyright page and the last page say.
 */

/** Newest printings first, then the ones with a page count — the useful ones. */
export function sortEditions(editions: BookEdition[]): BookEdition[] {
  return [...editions].sort((a, b) => {
    if (Boolean(a.page_count) !== Boolean(b.page_count)) return a.page_count ? -1 : 1;
    return (b.published_year ?? 0) - (a.published_year ?? 0);
  });
}

/** ISBN-10 → ISBN-13, or "" when the input is not a well-formed ISBN-10. */
export function isbn10To13(isbn10: string): string {
  const isbn = normalizeISBN(isbn10);
  if (!/^\d{9}[\dX]$/.test(isbn)) return "";
  const core = "978" + isbn.slice(0, 9);
  let sum = 0;
  for (let i = 0; i < 12; i++) sum += Number(core[i]) * (i % 2 === 0 ? 1 : 3);
  return core + String((10 - (sum % 10)) % 10);
}

/** ISBN-13 → ISBN-10, or "" for a 979 prefix (which has no ISBN-10). */
export function isbn13To10(isbn13: string): string {
  const isbn = normalizeISBN(isbn13);
  if (!/^978\d{10}$/.test(isbn)) return "";
  const core = isbn.slice(3, 12);
  let sum = 0;
  for (let i = 0; i < 9; i++) sum += Number(core[i]) * (10 - i);
  const check = (11 - (sum % 11)) % 11;
  return core + (check === 10 ? "X" : String(check));
}

/** Both forms of a printing's ISBN, whichever of them the catalogue carried. */
export function editionISBNs(edition: BookEdition): string[] {
  const thirteen = normalizeISBN(edition.isbn13) || isbn10To13(edition.isbn10);
  const ten = normalizeISBN(edition.isbn10) || isbn13To10(edition.isbn13);
  return [thirteen, ten].filter(Boolean);
}

/** Where a query's ISBN run landed in the ISBN shown, for highlighting. */
export interface ISBNHit {
  start: number;
  end: number;
}

export interface EditionMatch {
  edition: BookEdition;
  /** The span of `normalizeISBN(editionISBN(edition))` the query's digits matched, if any. */
  isbnHit: ISBNHit | null;
}

/**
 * Filters (and, for a query, ranks) printings. Every whitespace-separated
 * term must match somewhere; an ISBN hit ranks above a word hit, and an
 * exact ISBN above both — a scanned barcode should land on one row, first.
 */
export function matchEditions(editions: BookEdition[], query: string): EditionMatch[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return editions.map((edition) => ({ edition, isbnHit: null }));

  const scored: { match: EditionMatch; score: number; order: number }[] = [];
  editions.forEach((edition, order) => {
    const isbns = editionISBNs(edition).map((isbn) => isbn.toLowerCase());
    const shown = normalizeISBN(editionISBN(edition)).toLowerCase();
    const words = [
      edition.publisher,
      edition.binding,
      edition.language,
      edition.published_year ? String(edition.published_year) : "",
      formatPages(edition.page_count),
    ]
      .join(" ")
      .toLowerCase();

    let score = 0;
    let isbnHit: ISBNHit | null = null;
    for (const term of terms) {
      // Hyphens and the ISBN's X are part of how people type a number.
      const digits = term.replace(/-/g, "");
      const numeric = /^\d+x?$/.test(digits);
      const inISBN = numeric && isbns.some((isbn) => isbn.includes(digits));
      const inWords = words.includes(term);
      if (!inISBN && !inWords) return;
      // A long run of digits is an ISBN being typed; a short one is more
      // likely the page count or year, and only incidentally inside an ISBN.
      if (inISBN && (isbns.includes(digits) || digits.length >= 4)) {
        score += isbns.includes(digits) ? 100 : 10;
      } else {
        score += inWords ? 3 : 1;
      }
      if (inISBN) {
        const at = shown.indexOf(digits);
        if (at >= 0) isbnHit = { start: at, end: at + digits.length };
      }
    }
    scored.push({ match: { edition, isbnHit }, score, order });
  });

  return scored
    .sort((a, b) => b.score - a.score || a.order - b.order)
    .map(({ match }) => match);
}
