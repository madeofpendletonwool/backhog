import { cn } from "@/lib/cn";
import { Link } from "react-router-dom";

import { BookCover } from "./BookCover";
import { StatusMenu } from "./StatusMenu";
import { StatusBadge } from "./StatusBadge";
import { Gi } from "./ui/Gi";
import { accentStyle, byline, publishYear } from "@/lib/format";
import type { BookEntry } from "@/lib/types";

/**
 * A jacket-led card, the books mirror of GameCard: the shelf reads as a wall
 * of covers at rest and only reveals detail on intent. The author is the one
 * fact that earns a permanent line — a shelf sorted by author is unreadable
 * without it.
 */
export function BookCard({ entry }: { entry: BookEntry }) {
  const { book } = entry;
  const author = byline(book);
  const year = publishYear(book);

  return (
    <div className="group relative" style={accentStyle(book)}>
      {/* Accent glow, sampled from the jacket art. */}
      <div
        className="pointer-events-none absolute -inset-2 -z-10 rounded-2xl opacity-0 blur-xl transition-opacity duration-300 group-hover:opacity-45"
        style={{ background: "var(--accent)" }}
        aria-hidden="true"
      />

      <Link
        to={`/books/${entry.id}`}
        className="block rounded-xl focus-visible:focus-ring"
        aria-label={book.title}
      >
        <div
          className={cn(
            "relative overflow-hidden rounded-xl ring-1 ring-white/[0.08]",
            "transition-transform duration-300 ease-[var(--ease-spring)]",
            "group-hover:-translate-y-1 group-hover:ring-white/20",
          )}
        >
          <BookCover book={book} sizes="(max-width: 640px) 45vw, 200px" />

          {/* Bottom scrim carrying the title, always legible over any jacket. */}
          <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-ink-950 via-ink-950/85 to-transparent px-3 pb-2.5 pt-8">
            <p className="line-clamp-2 text-[13px] font-semibold leading-snug text-white">
              {book.title}
            </p>
            <div className="mt-1 flex items-center gap-2 text-[11px] text-ink-400">
              {author && <span className="min-w-0 truncate">{author}</span>}
              {year && <span className="shrink-0">{year}</span>}
            </div>
          </div>

          {entry.user_rating != null && (
            <div className="absolute right-2 top-2 inline-flex items-center gap-1 rounded-lg bg-ink-950/80 px-1.5 py-1 text-[11px] font-semibold text-amber-300 backdrop-blur-sm">
              <Gi name="star" className="size-3" />
              {entry.user_rating}
            </div>
          )}

          <div className="absolute left-2 top-2">
            <StatusBadge
              status={entry.status}
              media="book"
              showLabel={false}
              className="px-1.5 backdrop-blur-sm"
            />
          </div>
        </div>
      </Link>

      {/* Quick status switch, revealed on hover or keyboard focus. */}
      <div className="pointer-events-none absolute inset-x-2 bottom-2 opacity-0 transition-opacity duration-200 focus-within:pointer-events-auto focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100 [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100">
        <StatusMenu entry={entry} />
      </div>
    </div>
  );
}

export function BookCardSkeleton() {
  return (
    <div className="space-y-2">
      <div className="skeleton aspect-[2/3] w-full rounded-xl" />
    </div>
  );
}
