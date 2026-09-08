import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Gi } from "@/components/ui/Gi";
import { Button, Panel, Spinner } from "@/components/ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { api } from "@/lib/api";
import type { MediaFile, ShareCandidate } from "@/lib/types";

/**
 * Who else may read this book.
 *
 * A share is a grant against the files you attached: the other person reads
 * and listens through their own library entry, with their own place in the
 * book, their own reading sessions and their own rating. Nothing of yours is
 * visible to them and nothing of theirs to you.
 *
 * Revoking takes back the grant and nothing else. Their entry, their
 * progress and their notes stay exactly where they were — the book simply
 * goes back to being one with no files attached, and sharing it again later
 * picks up where they left off.
 *
 * The panel is only for books you have files on: sharing a book you have not
 * attached anything to grants nothing, and offering the button anyway would
 * be a promise the server cannot keep.
 */
export function ShareBookPanel({ entryId, files }: { entryId: string; files: MediaFile[] }) {
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const [error, setError] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["bookShares", entryId],
    queryFn: () => api.bookShares(entryId),
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["bookShares", entryId] });
    void queryClient.invalidateQueries({ queryKey: ["shares"] });
  };

  const share = useMutation({
    mutationFn: (userId: string) => api.shareBook(entryId, userId),
    onSuccess: () => {
      setError(null);
      refresh();
    },
    onError: (err: Error) => setError(err.message),
  });

  const unshare = useMutation({
    mutationFn: (userId: string) => api.unshareBook(entryId, userId),
    onSuccess: () => {
      setError(null);
      refresh();
    },
    onError: (err: Error) => setError(err.message),
  });

  // Only the person whose files these are can lend them: everyone else is
  // reading a borrowed copy. Attachment ownership is the whole test — a
  // reader cannot attach anything, so this excludes them on its own — and
  // the server scopes every one of these calls through an entry you own
  // regardless.
  const mine = files.some((file) => file.attached_by && file.attached_by === user?.id);
  if (!mine) return null;

  const candidates = data?.candidates ?? [];
  if (!isLoading && candidates.length === 0) return null;

  const busy = share.isPending || unshare.isPending;
  const sharedCount = candidates.filter((c) => c.shared).length;

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Lend this book</h2>
      <p className="mb-3 text-xs leading-relaxed text-ink-500">
        {sharedCount === 0
          ? "Give someone else on this server the ebook and the audiobook. They read it on their own shelf, at their own place in the book."
          : `Shared with ${sharedCount} ${sharedCount === 1 ? "person" : "people"}. They keep their own place in the book, and their own reading history.`}
      </p>

      {error && (
        <p role="alert" className="mb-3 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
          {error}
        </p>
      )}

      {isLoading ? (
        <Spinner className="size-5" />
      ) : (
        <ul className="space-y-2">
          {candidates.map((candidate) => (
            <ShareRow
              key={candidate.user_id}
              candidate={candidate}
              busy={busy}
              onToggle={() =>
                candidate.shared
                  ? unshare.mutate(candidate.user_id)
                  : share.mutate(candidate.user_id)
              }
            />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function ShareRow({
  candidate,
  busy,
  onToggle,
}: {
  candidate: ShareCandidate;
  busy: boolean;
  onToggle: () => void;
}) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-2">
      <span className="min-w-0">
        <span className="block truncate text-sm text-ink-200">{candidate.username}</span>
        {candidate.shared && !candidate.in_library && (
          // The share is live, but they have not put the book on their shelf
          // yet — a share grants access, it does not reach into someone
          // else's library and add rows to it.
          <span className="block text-xs text-ink-500">
            hasn&rsquo;t added it to their shelf yet
          </span>
        )}
      </span>
      <Button
        size="sm"
        variant={candidate.shared ? "ghost" : "primary"}
        disabled={busy}
        onClick={onToggle}
      >
        <Gi name={candidate.shared ? "x" : "gift"} className="size-3.5" />
        {candidate.shared ? "Stop sharing" : "Share"}
      </Button>
    </li>
  );
}
