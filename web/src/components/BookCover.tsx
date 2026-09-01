import { cn } from "@/lib/cn";
import { useState } from "react";

import { bookCoverUrl } from "@/lib/api";
import type { Book } from "@/lib/types";
import { Gi } from "./ui/Gi";

/**
 * A book cover with a graceful fallback, the mirror of GameCover. Jackets are
 * 2:3 rather than a game box's 3:4 — the shelf reads as books at a glance,
 * and a real cover never gets letterboxed into the wrong proportion.
 *
 * Works with no artwork get the same tinted monogram tile games get, using
 * the accent sampled from whatever cover Open Library did have.
 */
export function BookCover({
  book,
  className,
  sizes,
}: {
  book: Book;
  className?: string;
  sizes?: string;
}) {
  const [failed, setFailed] = useState(false);
  const showFallback = failed || !book.cover_url;

  return (
    <div
      className={cn(
        "relative isolate aspect-[2/3] w-full overflow-hidden rounded-xl bg-ink-800",
        className,
      )}
    >
      {showFallback ? (
        <div
          className="flex size-full flex-col items-center justify-center gap-2 p-3 text-center"
          style={{
            background:
              "linear-gradient(160deg, color-mix(in oklab, var(--accent) 28%, var(--color-ink-800)), var(--color-ink-850))",
          }}
        >
          <Gi name="book-pile" className="size-7 text-white/35" />
          <span className="line-clamp-3 text-[11px] font-medium leading-tight text-white/60">
            {book.title}
          </span>
        </div>
      ) : (
        <img
          src={bookCoverUrl(book.id)}
          alt=""
          loading="lazy"
          decoding="async"
          sizes={sizes}
          onError={() => setFailed(true)}
          className="size-full object-cover"
        />
      )}
    </div>
  );
}
