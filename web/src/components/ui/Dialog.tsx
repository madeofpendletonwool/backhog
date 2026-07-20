import { cn } from "@/lib/cn";
import { X } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";

interface DialogProps {
  open: boolean;
  onClose: () => void;
  children: ReactNode;
  className?: string;
  /** Set for the command palette, which supplies its own chrome. */
  bare?: boolean;
  label: string;
}

/**
 * A modal dialog. Handles Escape, backdrop clicks, body scroll locking and
 * focus restoration — the parts that are easy to leave out and immediately
 * noticeable when missing.
 */
export function Dialog({ open, onClose, children, className, bare, label }: DialogProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const restoreFocusTo = useRef<HTMLElement | null>(null);

  // Held in a ref so the setup effect below can depend on `open` alone.
  // Callers routinely pass an inline arrow function, whose identity changes on
  // every render — including every keystroke into an input inside the dialog.
  // If the effect depended on it, each keystroke would tear down and re-run
  // this setup, and the cleanup would yank focus back to whatever opened the
  // dialog. That reads as "the text box deselects itself as I type".
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return;

    restoreFocusTo.current = document.activeElement as HTMLElement | null;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        onCloseRef.current();
      }
    };
    document.addEventListener("keydown", onKeyDown);

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    // Move focus into the dialog so keyboard users are not left behind it.
    const focusable = panelRef.current?.querySelector<HTMLElement>(
      "input, button, [tabindex]:not([tabindex='-1'])",
    );
    focusable?.focus();

    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
      restoreFocusTo.current?.focus?.();
    };
  }, [open]);

  if (!open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto p-4 pt-[10vh]"
      role="dialog"
      aria-modal="true"
      aria-label={label}
    >
      <div
        className="fixed inset-0 bg-ink-950/80 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        ref={panelRef}
        className={cn(
          "animate-fade-rise relative w-full",
          !bare && "panel p-6",
          className ?? "max-w-lg",
        )}
      >
        {!bare && (
          <button
            onClick={onClose}
            aria-label="Close"
            className="absolute right-4 top-4 rounded-lg p-1.5 text-ink-500 transition-colors hover:bg-white/[0.06] hover:text-ink-200 focus-visible:focus-ring"
          >
            <X className="size-4" />
          </button>
        )}
        {children}
      </div>
    </div>,
    document.body,
  );
}
