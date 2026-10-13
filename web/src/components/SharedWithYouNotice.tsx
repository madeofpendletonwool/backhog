import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";

import { Gi } from "@/components/ui/Gi";
import { Button } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import type { BookShare } from "@/lib/types";

/** Often enough that a share lands while the tab is open, rarely enough to cost nothing. */
const POLL_MS = 60_000;

/**
 * Tells you when someone lends you a book.
 *
 * A share grants access but never puts rows in your library, so without
 * this the only way to find out was to add the book yourself and notice it
 * came with files. Every share you have neither shelved nor dismissed shows
 * here, above whatever page you are on, with the one click that shelves it.
 * Dismissing only hides the notice: the share stays, and Settings still
 * lists it under "Lent to you".
 */
export function SharedWithYouNotice() {
  const { data } = useQuery({
    queryKey: ["shares"],
    queryFn: api.shares,
    refetchInterval: POLL_MS,
  });

  const pending = (data?.received ?? []).filter((share) => !share.seen && !share.in_library);
  if (pending.length === 0) return null;

  return (
    <div className="mx-auto max-w-5xl px-4 pt-4 sm:px-6 lg:px-8">
      <section
        aria-label="Books lent to you"
        className="f-panel animate-fade-rise space-y-2 p-3"
      >
        {pending.map((share) => (
          <NoticeRow key={share.id} share={share} />
        ))}
      </section>
    </div>
  );
}

function NoticeRow({ share }: { share: BookShare }) {
  const queryClient = useQueryClient();
  const [error, setError] = useState("");
  // Set once the book is on the shelf, so the row can offer to open it
  // before it leaves.
  const [entryId, setEntryId] = useState<string | null>(null);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["shares"] });
  };

  const dismiss = useMutation({
    mutationFn: () => api.markShareSeen(share.id),
    onSuccess: refresh,
    onError: (err: Error) => setError(err.message),
  });

  const shelve = useMutation({
    mutationFn: async () => {
      const entry = await api.addBookToLibrary(share.book_id);
      await api.markShareSeen(share.id);
      return entry;
    },
    onSuccess: (entry) => {
      setError("");
      setEntryId(entry.id);
      void queryClient.invalidateQueries({ queryKey: ["library"] });
      void queryClient.invalidateQueries({ queryKey: ["books"] });
    },
    onError: (err: Error) => setError(err.message),
  });

  const busy = dismiss.isPending || shelve.isPending;

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
      <div className="f-chip flex size-9 shrink-0 items-center justify-center text-hl-bright">
        <Gi name="gift" className="size-4" />
      </div>
      <p className="min-w-0 flex-1 text-sm text-ink-300">
        <span className="font-semibold text-ink-100">{share.owner_username}</span> lent you{" "}
        <span className="font-semibold text-ink-100">{share.book_title || "a book"}</span>
        {entryId && <span className="text-ink-500"> — it&rsquo;s on your shelf.</span>}
        {error && (
          <span role="alert" className="block text-xs text-red-300">
            {error}
          </span>
        )}
      </p>
      {entryId ? (
        <>
          <Link
            to={`/books/${entryId}`}
            onClick={refresh}
            className="text-sm font-medium text-hl-bright hover:underline"
          >
            Open it
          </Link>
          <Button size="sm" variant="ghost" onClick={refresh}>
            Done
          </Button>
        </>
      ) : (
        <>
          <Button size="sm" loading={shelve.isPending} disabled={busy} onClick={() => shelve.mutate()}>
            <Gi name="plus" className="size-3.5" />
            Add to shelf
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => dismiss.mutate()}>
            Dismiss
          </Button>
        </>
      )}
    </div>
  );
}
