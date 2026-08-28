import { cn } from "@/lib/cn";

import { useUpdateEntry } from "@/hooks/useLibrary";
import { QUICK_STATUSES, STATUS_LABELS, type Entry, type Status } from "@/lib/types";
import { STATUS_ICONS } from "./StatusBadge";
import { Gi } from "./ui/Gi";

const activeStyles: Record<Status, string> = {
  backlog: "bg-slate-500 text-white",
  playing: "bg-cyan-500 text-ink-950",
  played: "bg-emerald-500 text-ink-950",
  dropped: "bg-red-500 text-white",
  ignored: "bg-zinc-500 text-white",
  wishlist: "bg-amber-500 text-ink-950",
};

/**
 * A compact segmented control for changing an entry's status straight from the
 * grid, so the most common action never needs a page change.
 *
 * `statuses` defaults to the quick-access set (no wishlist). The detail page
 * passes the full list so wishlist can be set there.
 */
export function StatusMenu({
  entry,
  size = "sm",
  statuses = QUICK_STATUSES,
}: {
  entry: Entry;
  size?: "sm" | "md";
  statuses?: Status[];
}) {
  const update = useUpdateEntry();

  return (
    <div
      role="group"
      aria-label={`Status for ${entry.game.name}`}
      className={cn(
        "gap-0.5 bg-ink-900/95 p-1 ring-1 ring-white/10 backdrop-blur-md",
        size === "md" ? "grid grid-cols-3 sm:flex sm:items-center" : "flex items-center",
        update.isPending && "opacity-60",
      )}
    >
      {statuses.map((status) => {
        const isActive = entry.status === status;
        return (
          <button
            key={status}
            type="button"
            title={STATUS_LABELS[status]}
            aria-label={STATUS_LABELS[status]}
            aria-pressed={isActive}
            disabled={update.isPending}
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              if (isActive) return;
              update.mutate({ id: entry.id, patch: { status } });
            }}
            className={cn(
              "flex flex-1 items-center justify-center transition-colors",
              size === "sm" ? "h-7" : "h-8 px-3",
              isActive
                ? activeStyles[status]
                : "text-ink-400 hover:bg-white/[0.08] hover:text-ink-100",
              "focus-visible:focus-ring disabled:cursor-not-allowed",
            )}
          >
            <Gi name={STATUS_ICONS[status]} className="size-3.5" />
            {size === "md" && <span className="ml-1.5 text-xs font-medium">{STATUS_LABELS[status]}</span>}
          </button>
        );
      })}
    </div>
  );
}
