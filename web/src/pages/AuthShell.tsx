import type { ReactNode } from "react";

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
  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-12">
      <div className="animate-fade-rise w-full max-w-sm">
        <div className="mb-8 text-center">
          <span className="text-5xl">🐗</span>
          <h1 className="mt-4 text-2xl font-semibold tracking-tight text-ink-100">{title}</h1>
          <p className="mt-1.5 text-sm text-ink-400">{subtitle}</p>
        </div>

        <div className="panel p-6">{children}</div>

        <p className="mt-6 text-center text-sm text-ink-400">{footer}</p>
      </div>
    </div>
  );
}
