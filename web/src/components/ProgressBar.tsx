import { cn } from "@/lib/cn";

/** A thin progress bar; emerald when the target is met, brand while working. */
export function ProgressBar({ percent, complete }: { percent: number; complete: boolean }) {
  return (
    <div
      className="h-1.5 w-full overflow-hidden rounded-full bg-white/[0.07]"
      role="progressbar"
      aria-valuenow={Math.round(percent)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div
        className={cn(
          "h-full rounded-full transition-[width] duration-500",
          complete ? "bg-emerald-400" : "bg-brand-500",
        )}
        style={{ width: `${Math.min(100, Math.max(0, percent))}%` }}
      />
    </div>
  );
}
