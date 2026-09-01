import { cn } from "@/lib/cn";
import { Gi } from "./Gi";
import { useTheme, type ThemeFamily } from "@/hooks/useTheme";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
type ButtonSize = "sm" | "md" | "lg" | "icon";

/* Chrome — frames, radii, colour — is the family's job and lives in CSS
   (themes/flat.css, pixel/pixel.css); the class names below are the same
   in both. What CSS cannot swap is *metrics and type*: the arcade's frames
   are 12px of sprite, so its controls have to be taller, and its label
   face is a 5px bitmap that only reads uppercase at 11px. Those are the
   only things this file branches on. */
const buttonVariants: Record<ThemeFamily, Record<ButtonVariant, string>> = {
  pixel: {
    primary: "f-btn-gold font-display text-[11px] uppercase tracking-wider",
    secondary: "f-btn-soft font-display text-[11px] uppercase tracking-wider",
    ghost: "text-ink-300 hover:text-ink-100 hover:bg-white/[0.06]",
    danger: "f-btn-danger font-display text-[11px] uppercase tracking-wider",
  },
  flat: {
    primary: "f-btn-gold",
    secondary: "f-btn-soft",
    ghost: "rounded-xl text-ink-300 hover:text-ink-100 hover:bg-white/[0.06]",
    danger: "f-btn-danger",
  },
};

const buttonBase: Record<ThemeFamily, string> = {
  // The arcade button sinks by a pixel; the flat one squashes.
  pixel: "font-bold active:translate-y-px disabled:active:translate-y-0",
  flat: "font-medium active:scale-[0.98] disabled:active:scale-100",
};

const buttonSizes: Record<ThemeFamily, Record<ButtonSize, string>> = {
  pixel: {
    sm: "h-10 px-3 gap-1.5",
    md: "h-12 px-4 gap-2",
    lg: "h-14 px-5 gap-2",
    icon: "h-11 w-11",
  },
  flat: {
    sm: "h-8 px-3 text-xs gap-1.5",
    md: "h-10 px-4 text-sm gap-2",
    lg: "h-11 px-5 text-sm gap-2",
    icon: "h-9 w-9",
  },
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
  const { family } = useTheme();
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center",
        "transition-all duration-150 ease-[var(--ease-spring)]",
        "focus-visible:focus-ring",
        "disabled:cursor-not-allowed disabled:opacity-60",
        buttonBase[family],
        buttonVariants[family][variant],
        buttonSizes[family][size],
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

/** "Working on it", inline: a blinking coin light in the arcade, a
 *  pulsing dot in Midnight. Both take a `bg-*` from the caller, so the
 *  mark can pick up the colour of whatever it sits inside. */
export function LoadingDot({ className }: { className?: string }) {
  const { family } = useTheme();
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-block size-2 rounded-[2px] bg-current",
        family === "flat" ? "dot-pulse rounded-full" : "sprite-loading",
        className,
      )}
    />
  );
}

/** "Working on it", standing alone — big enough to be the only thing on
 *  screen, so Midnight gets a real spinning ring rather than a dot. */
export function Spinner({ className }: { className?: string }) {
  const { family } = useTheme();
  if (family === "flat") {
    return <span aria-hidden="true" className={cn("spin-ring size-5 text-ink-400", className)} />;
  }
  return <LoadingDot className={cn("size-2.5 bg-ink-400", className)} />;
}

/** Field metrics follow the frame: the arcade's is 8px of sprite a side. */
const fieldSize: Record<ThemeFamily, string> = {
  pixel: "h-12 px-3",
  flat: "h-10 px-3.5",
};

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  const { family } = useTheme();
  return (
    <input
      className={cn(
        "f-field w-full text-sm",
        fieldSize[family],
        "focus:outline-none focus-visible:focus-ring",
        className,
      )}
      {...props}
    />
  );
}

export function Select({ className, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  const { family } = useTheme();
  return (
    <select
      className={cn(
        "f-field w-full appearance-none text-sm",
        fieldSize[family],
        family === "flat" && "px-3",
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
  const { family } = useTheme();
  return (
    <div className="animate-fade-rise flex flex-col items-center justify-center px-6 py-20 text-center">
      <div
        className={cn(
          "mb-5 flex size-16 items-center justify-center",
          family === "flat"
            ? "rounded-2xl bg-ink-850 text-ink-500 ring-1 ring-white/[0.06]"
            : "f-chip text-ink-400",
        )}
      >
        {icon}
      </div>
      <h3
        className={cn(
          "text-ink-100",
          family === "flat"
            ? "text-lg font-semibold"
            : "font-display text-sm uppercase tracking-wider",
        )}
      >
        {title}
      </h3>
      <p className="mt-1.5 max-w-sm text-sm leading-relaxed text-ink-400">{description}</p>
      {action && <div className="mt-6">{action}</div>}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  /* rounded-xl is 2px of arcade plastic and 14px of Midnight — the same
     class, resolved by the family's --r-xl. */
  return <div className={cn("skeleton rounded-xl", className)} />;
}

/** Section label — the console's little uppercase placard. */
export function Label({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <p
      className={cn(
        "font-display text-[10px] font-bold uppercase tracking-widest text-ink-400",
        className,
      )}
    >
      {children}
    </p>
  );
}

export { Gi };
