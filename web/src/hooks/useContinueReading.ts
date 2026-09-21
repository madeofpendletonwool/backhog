import { useCallback } from "react";
import { useNavigate } from "react-router-dom";

import { useAudioPlayer } from "./useAudioPlayer";
import type { BookEntry } from "@/lib/types";

/**
 * The one move that every "continue" control makes: pick the book up the
 * way it was put down. A book last touched by the player resumes in the
 * player; everything else opens in the reader at the stored offset. The
 * decision comes from the entry's own `progress_source`, so a shelf card can
 * make it without a position call apiece — and a book that has never been
 * opened simply goes to the reader, which says so if there is nothing to read.
 */
export function useContinueReading() {
  const navigate = useNavigate();
  const player = useAudioPlayer();

  return useCallback(
    (entry: BookEntry) => {
      if (entry.progress_source === "listen") {
        if (player.entry?.id === entry.id) {
          if (!player.playing) player.toggle();
          return;
        }
        player.open(entry, { autoplay: true });
        return;
      }
      navigate(`/books/${entry.id}/read`);
    },
    [navigate, player],
  );
}

/** The label a "continue" control wears for this entry. */
export function continueLabel(entry: BookEntry): string {
  if (entry.progress_source === "listen") return "Continue listening";
  return entry.progress_percent && entry.progress_percent > 0 ? "Continue reading" : "Start reading";
}
