import { useSearchParams } from "react-router-dom";

import { cn } from "@/lib/cn";
import type { MediaFilterValue } from "@/lib/entry";
import { Gi } from "./ui/Gi";
import type { GiName } from "@/lib/gameicons";

/**
 * The queue, lists and projects are shared across both arenas — one ordered
 * collection that can hold a game and a book side by side — so they get a
 * filter rather than a second copy of themselves under /books.
 *
 * The filter lives in the URL, not in localStorage: it is what makes the books
 * nav's "Reading Queue" a different link from the games nav's "Play Queue"
 * while both point at the same page. No parameter means games, which is what
 * those pages showed before books existed.
 */
export function useMediaFilter(): [MediaFilterValue, (next: MediaFilterValue) => void] {
  const [params, setParams] = useSearchParams();
  const raw = params.get("media");
  const value: MediaFilterValue = raw === "book" || raw === "all" ? raw : "game";

  const set = (next: MediaFilterValue) => {
    const updated = new URLSearchParams(params);
    if (next === "game") updated.delete("media");
    else updated.set("media", next);
    // replace: flipping a filter is not a place you want to land on Back.
    setParams(updated, { replace: true });
  };

  return [value, set];
}

const segments: { value: MediaFilterValue; label: string; icon: GiName }[] = [
  { value: "game", label: "Games", icon: "gamepad" },
  { value: "book", label: "Books", icon: "book-pile" },
  { value: "all", label: "Both", icon: "layers" },
];

export function MediaFilter({
  value,
  onChange,
  className,
}: {
  value: MediaFilterValue;
  onChange: (next: MediaFilterValue) => void;
  className?: string;
}) {
  return (
    <div
      role="group"
      aria-label="Show"
      className={cn("flex rounded-xl border border-white/[0.07] bg-ink-850 p-0.5", className)}
    >
      {segments.map((segment) => (
        <button
          key={segment.value}
          type="button"
          onClick={() => onChange(segment.value)}
          aria-pressed={value === segment.value}
          className={cn(
            "inline-flex items-center gap-1.5 rounded-[0.6rem] px-2.5 py-1.5 text-xs font-medium transition-colors focus-visible:focus-ring",
            value === segment.value
              ? "bg-white/[0.09] text-ink-100"
              : "text-ink-500 hover:text-ink-300",
          )}
        >
          <Gi name={segment.icon} className="size-3.5" />
          {segment.label}
        </button>
      ))}
    </div>
  );
}
