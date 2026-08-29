import { cn } from "@/lib/cn";

/** A framed progress bar; emerald when the target is met, the accent while
 *  working — the fill is the one place inside the frame the accent shows. */
export function ProgressBar({ percent, complete }: { percent: number; complete: boolean }) {
  return (
    <div
      className="f-bar h-4 w-full overflow-hidden"
      role="progressbar"
      aria-valuenow={Math.round(percent)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div
        className={cn("h-full transition-[width] duration-500", complete && "bg-emerald-400")}
        style={{
          width: `${Math.min(100, Math.max(0, percent))}%`,
          background: complete ? undefined : "var(--accent)",
        }}
      />
    </div>
  );
}
