import { cn } from "@/lib/cn";
import { Gi } from "./Gi";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
type ButtonSize = "sm" | "md" | "lg" | "icon";

/* Frames are nine-slice sprites (pixel.css); a framed surface carries no
   background, radius, or shadow of its own. Height classes account for the
   frame border so all variants line up. */
const buttonVariants: Record<ButtonVariant, string> = {
  primary: "f-btn-gold font-pixel text-[11px] uppercase tracking-wider",
  secondary:
    "f-btn-soft font-pixel text-[11px] uppercase tracking-wider",
  ghost: "text-ink-300 hover:text-ink-100 hover:bg-white/[0.06]",
  danger: "f-btn-danger font-pixel text-[11px] uppercase tracking-wider",
};

const buttonSizes: Record<ButtonSize, string> = {
  sm: "h-10 px-3 gap-1.5",
  md: "h-12 px-4 gap-2",
  lg: "h-14 px-5 gap-2",
  icon: "h-11 w-11",
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
        "inline-flex items-center justify-center font-bold",
        "transition-all duration-150 ease-[var(--ease-spring)]",
        "focus-visible:focus-ring active:translate-y-px",
        "disabled:cursor-not-allowed disabled:opacity-60 disabled:active:translate-y-0",
        buttonVariants[variant],
        buttonSizes[size],
        className,
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <LoadingDot />}
      {children}
    </button>
  );
}

/** The arcade mark of "working on it": a blinking coin light. */
export function LoadingDot({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "sprite-loading inline-block size-2 rounded-[2px] bg-current",
        className,
      )}
    />
  );
}

export function Spinner({ className }: { className?: string }) {
  return <LoadingDot className={cn("size-2.5 bg-ink-400", className)} />;
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "f-field h-12 w-full px-3 text-sm",
        "transition-[border-image-source] focus:outline-none focus-visible:focus-ring",
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
        "f-field h-12 w-full appearance-none px-3 text-sm",
        "focus:outline-none focus-visible:focus-ring",
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
      <div className="f-chip mb-5 flex size-16 items-center justify-center text-ink-400">
        {icon}
      </div>
      <h3 className="font-pixel text-sm uppercase tracking-wider text-ink-100">{title}</h3>
      <p className="mt-1.5 max-w-sm text-sm leading-relaxed text-ink-400">{description}</p>
      {action && <div className="mt-6">{action}</div>}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("skeleton", className)} />;
}

/** Section label — the console's little uppercase placard. */
export function Label({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <p
      className={cn(
        "font-pixel text-[10px] font-bold uppercase tracking-widest text-ink-400",
        className,
      )}
    >
      {children}
    </p>
  );
}

export { Gi };
