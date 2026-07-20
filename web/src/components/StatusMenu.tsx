import { cn } from "@/lib/cn";
import { CheckCircle2, CircleDashed, Gift, PlayCircle, XCircle } from "lucide-react";

import { useUpdateEntry } from "@/hooks/useLibrary";
import { STATUS_LABELS, STATUSES, type Entry, type Status } from "@/lib/types";

const icons: Record<Status, typeof CircleDashed> = {
  backlog: CircleDashed,
  playing: PlayCircle,
  played: CheckCircle2,
  dropped: XCircle,
  wishlist: Gift,
};

const activeStyles: Record<Status, string> = {
  backlog: "bg-slate-500 text-white",
  playing: "bg-cyan-500 text-ink-950",
  played: "bg-emerald-500 text-ink-950",
  dropped: "bg-red-500 text-white",
  wishlist: "bg-amber-500 text-ink-950",
};

/**
 * A compact segmented control for changing an entry's status straight from the
 * grid, so the most common action never needs a page change.
 */
export function StatusMenu({ entry, size = "sm" }: { entry: Entry; size?: "sm" | "md" }) {
  const update = useUpdateEntry();

  return (
    <div
      role="group"
      aria-label={`Status for ${entry.game.name}`}
      className={cn(
        "flex items-center gap-0.5 rounded-xl bg-ink-900/95 p-1 ring-1 ring-white/10 backdrop-blur-md",
        update.isPending && "opacity-60",
      )}
    >
      {STATUSES.map((status) => {
        const Icon = icons[status];
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
              "flex flex-1 items-center justify-center rounded-lg transition-colors",
              size === "sm" ? "h-7" : "h-8 px-3",
              isActive
                ? activeStyles[status]
                : "text-ink-400 hover:bg-white/[0.08] hover:text-ink-100",
              "focus-visible:focus-ring disabled:cursor-not-allowed",
            )}
          >
            <Icon className="size-3.5" />
            {size === "md" && <span className="ml-1.5 text-xs font-medium">{STATUS_LABELS[status]}</span>}
          </button>
        );
      })}
    </div>
  );
}
