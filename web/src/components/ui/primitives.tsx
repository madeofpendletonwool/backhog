import { cn } from "@/lib/cn";
import { Loader2 } from "lucide-react";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
type ButtonSize = "sm" | "md" | "lg" | "icon";

const buttonVariants: Record<ButtonVariant, string> = {
  primary:
    "bg-brand-600 text-white hover:bg-brand-500 shadow-lg shadow-brand-600/25 disabled:bg-ink-700 disabled:shadow-none",
  secondary:
    "bg-ink-800 text-ink-100 border border-white/[0.07] hover:bg-ink-750 hover:border-white/[0.12]",
  ghost: "text-ink-300 hover:text-ink-100 hover:bg-white/[0.06]",
  danger: "bg-red-600/90 text-white hover:bg-red-600",
};

const buttonSizes: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-xs gap-1.5",
  md: "h-10 px-4 text-sm gap-2",
  lg: "h-11 px-5 text-sm gap-2",
  icon: "h-9 w-9",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}

export function Button({
  variant = "secondary",
  size = "md",
  loading = false,
  className,
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center rounded-xl font-medium",
        "transition-all duration-150 ease-[var(--ease-spring)]",
        "focus-visible:focus-ring active:scale-[0.98]",
        "disabled:cursor-not-allowed disabled:opacity-60 disabled:active:scale-100",
        buttonVariants[variant],
        buttonSizes[size],
        className,
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <Loader2 className="size-4 animate-spin" />}
      {children}
    </button>
  );
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "h-10 w-full rounded-xl border border-white/[0.07] bg-ink-850 px-3.5 text-sm",
        "text-ink-100 placeholder:text-ink-500",
        "transition-colors focus:border-brand-500/50 focus-visible:focus-ring",
        className,
      )}
      {...props}
    />
  );
}

export function Select({ className, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        "h-10 w-full appearance-none rounded-xl border border-white/[0.07] bg-ink-850 px-3 text-sm",
        "text-ink-100 transition-colors focus:border-brand-500/50 focus-visible:focus-ring",
        className,
      )}
      {...props}
    >
      {children}
    </select>
  );
}

export function Panel({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("panel", className)}>{children}</div>;
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn("size-5 animate-spin text-ink-400", className)} />;
}

/** A centred message for empty collections, with an optional call to action. */
export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="animate-fade-rise flex flex-col items-center justify-center px-6 py-20 text-center">
      <div className="mb-5 flex size-16 items-center justify-center rounded-2xl bg-ink-850 text-ink-500 ring-1 ring-white/[0.06]">
        {icon}
      </div>
      <h3 className="text-lg font-semibold text-ink-100">{title}</h3>
      <p className="mt-1.5 max-w-sm text-sm leading-relaxed text-ink-400">{description}</p>
      {action && <div className="mt-6">{action}</div>}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("skeleton rounded-xl", className)} />;
}
