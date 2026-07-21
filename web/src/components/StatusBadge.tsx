import { cn } from "@/lib/cn";
import { Ban, CheckCircle2, CircleDashed, Gift, PlayCircle, XCircle } from "lucide-react";

import { STATUS_LABELS, type Status } from "@/lib/types";

const statusStyles: Record<Status, string> = {
  backlog: "bg-slate-500/15 text-slate-300 ring-slate-400/25",
  playing: "bg-cyan-500/15 text-cyan-300 ring-cyan-400/30",
  played: "bg-emerald-500/15 text-emerald-300 ring-emerald-400/30",
  dropped: "bg-red-500/15 text-red-300 ring-red-400/30",
  ignored: "bg-zinc-500/15 text-zinc-300 ring-zinc-400/25",
  wishlist: "bg-amber-500/15 text-amber-300 ring-amber-400/30",
};

const statusIcons: Record<Status, typeof CircleDashed> = {
  backlog: CircleDashed,
  playing: PlayCircle,
  played: CheckCircle2,
  dropped: XCircle,
  ignored: Ban,
  wishlist: Gift,
};

export function StatusBadge({
  status,
  className,
  showLabel = true,
}: {
  status: Status;
  className?: string;
  showLabel?: boolean;
}) {
  const Icon = statusIcons[status];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        statusStyles[status],
        className,
      )}
    >
      <Icon className="size-3" />
      {showLabel && STATUS_LABELS[status]}
    </span>
  );
}

export const STATUS_DOT: Record<Status, string> = {
  backlog: "bg-slate-400",
  playing: "bg-cyan-400",
  played: "bg-emerald-400",
  dropped: "bg-red-400",
  ignored: "bg-zinc-400",
  wishlist: "bg-amber-400",
};
