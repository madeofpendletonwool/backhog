import type { ReactNode } from "react";
import { Sprite } from "@/components/ui/Sprite";

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
          <Sprite name="ball" scale={2} className="mx-auto" />
          <h1 className="mt-4 font-pixel text-xl uppercase tracking-widest text-ink-100">{title}</h1>
          <p className="mt-2 text-sm text-ink-400">{subtitle}</p>
        </div>

        <div className="panel p-6">{children}</div>

        <p className="mt-6 text-center text-sm text-ink-400">{footer}</p>
      </div>
    </div>
  );
}
