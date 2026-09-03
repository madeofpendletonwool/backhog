import type { ReactNode } from "react";
import { useTheme, type ThemeFamily } from "@/hooks/useTheme";
import { cn } from "@/lib/cn";

/** The one heading on the page, in each family's display voice. */
const titleClass: Record<ThemeFamily, string> = {
  pixel: "font-display text-xl uppercase tracking-widest",
  flat: "text-2xl font-semibold tracking-tight",
  library: "font-display text-3xl tracking-tight",
};

/** Shared chrome for the login and register pages. */
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
  footer: ReactNode;
}) {
  const { family } = useTheme();
  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-12">
      <div className="animate-fade-rise w-full max-w-sm">
        <div className="mb-8 text-center">
          {family === "pixel" ? (
            <span
              className="mark-hog mx-auto"
              style={{ "--s": 2 } as React.CSSProperties}
            />
          ) : (
            <span className="text-5xl">🐗</span>
          )}
          <h1 className={cn("mt-4 text-ink-100", titleClass[family])}>
            {title}
          </h1>
          <p className="mt-2 text-sm text-ink-400">{subtitle}</p>
        </div>

        <div className="panel p-6">{children}</div>

        <p className="mt-6 text-center text-sm text-ink-400">{footer}</p>
      </div>
    </div>
  );
}
