import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Dialog } from "@/components/ui/Dialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Skeleton } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import { cn } from "@/lib/cn";
import type { Book, BookCoverOption } from "@/lib/types";

/**
 * Every cover Open Library holds for a work, to swap out the one search
 * happened to promote. The thumbnails load straight from Open Library at
 * medium size (the server only ever caches the one chosen); `default=false`
 * makes a cover with no image 404 instead of serving a blank, so it can drop
 * out of the grid rather than sit there as an empty tile.
 */
export function CoverPicker({
  book,
  open,
  onClose,
}: {
  book: Book;
  open: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<number | null>(null);
  const [broken, setBroken] = useState<Set<number>>(() => new Set());

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["bookCovers", book.id],
    queryFn: () => api.bookCovers(book.id),
    enabled: open,
    staleTime: 10 * 60 * 1000,
  });

  const apply = useMutation({
    mutationFn: (coverId: number) => api.setBookCover(book.id, coverId),
    onSuccess: (updated, coverId) => {
      queryClient.setQueryData(["book", book.id], (prev: Book | undefined) =>
        prev ? { ...prev, ...updated } : updated,
      );
      queryClient.setQueryData(["bookCovers", book.id], (prev: typeof data) =>
        prev ? { ...prev, current_id: coverId } : prev,
      );
      // The work is embedded in every shelf, card and dashboard projection;
      // a cover change is rare enough to just refresh them all.
      queryClient.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== "bookCovers" && q.queryKey[0] !== "book",
      });
      close();
    },
  });

  const close = () => {
    setSelected(null);
    apply.reset();
    onClose();
  };

  const current = data?.current_id ?? 0;
  // The one it wears now leads, so "what am I replacing" is never a scroll away.
  const covers = (data?.covers ?? [])
    .filter((cover) => !broken.has(cover.id))
    .sort((a, b) => Number(b.id === current) - Number(a.id === current));
  const choice = selected ?? current;

  return (
    <Dialog open={open} onClose={close} label="Choose a cover" className="max-w-2xl">
      <h2 className="pr-8 text-lg font-semibold text-ink-100">Choose a cover</h2>
      <p className="mt-1 text-sm text-ink-400">
        Every cover Open Library has for {book.title}. The one you pick shows for everyone on
        this server.
      </p>

      <div className="-mx-2 mt-4 max-h-[60vh] overflow-y-auto px-2 py-1">
        {isLoading ? (
          <div className="grid grid-cols-3 gap-3 sm:grid-cols-4">
            {Array.from({ length: 8 }, (_, i) => (
              <Skeleton key={i} className="aspect-[2/3] w-full rounded-lg" />
            ))}
          </div>
        ) : isError ? (
          <div className="py-10 text-center text-sm text-ink-400">
            <p>Couldn't reach Open Library.</p>
            <Button size="sm" className="mt-3" onClick={() => refetch()}>
              <Gi name="refresh" className="size-3.5" />
              Try again
            </Button>
          </div>
        ) : covers.length === 0 ? (
          <p className="py-10 text-center text-sm text-ink-400">
            Open Library has no covers for this book.
          </p>
        ) : (
          <div className="grid grid-cols-3 gap-3 sm:grid-cols-4">
            {covers.map((cover) => (
              <CoverTile
                key={cover.id}
                cover={cover}
                current={cover.id === current}
                selected={cover.id === choice}
                onSelect={() => setSelected(cover.id)}
                onBroken={() => setBroken((prev) => new Set(prev).add(cover.id))}
              />
            ))}
          </div>
        )}
      </div>

      {apply.isError && (
        <p className="mt-3 text-sm text-red-400">
          {apply.error instanceof Error ? apply.error.message : "Couldn't set that cover."}
        </p>
      )}

      <div className="mt-5 flex flex-wrap justify-end gap-2">
        <Button variant="ghost" onClick={close}>
          Cancel
        </Button>
        <Button
          variant="primary"
          disabled={selected === null || selected === current}
          loading={apply.isPending}
          onClick={() => selected !== null && apply.mutate(selected)}
        >
          Use this cover
        </Button>
      </div>
    </Dialog>
  );
}

function CoverTile({
  cover,
  current,
  selected,
  onSelect,
  onBroken,
}: {
  cover: BookCoverOption;
  current: boolean;
  selected: boolean;
  onSelect: () => void;
  onBroken: () => void;
}) {
  const thumb = `${cover.url.replace(/-L\.jpg$/, "-M.jpg")}?default=false`;
  const caption = [cover.year, cover.publisher].filter(Boolean).join(" · ");
  const detail = [cover.binding, cover.language].filter(Boolean).join(" · ");

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className="group flex min-w-0 flex-col text-left focus-visible:outline-none"
    >
      <div
        className={cn(
          "relative aspect-[2/3] w-full overflow-hidden rounded-lg bg-ink-800 ring-2 transition-all",
          selected ? "ring-brand-400" : "ring-transparent group-hover:ring-edge-strong",
          "group-focus-visible:focus-ring",
        )}
      >
        <img
          src={thumb}
          alt=""
          loading="lazy"
          decoding="async"
          onError={onBroken}
          className="size-full object-cover"
        />
        {current && (
          <span className="absolute left-1.5 top-1.5 rounded-md bg-ink-950/85 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-ink-100">
            Current
          </span>
        )}
        {selected && !current && (
          <span className="absolute right-1.5 top-1.5 flex size-6 items-center justify-center rounded-full bg-brand-500 text-white">
            <Gi name="check" className="size-3.5" />
          </span>
        )}
      </div>
      <span className="mt-1.5 truncate text-xs text-ink-300">{caption || "Open Library"}</span>
      {detail && <span className="truncate text-[11px] text-ink-500">{detail}</span>}
    </button>
  );
}
