import { Component, type ErrorInfo, type ReactNode } from "react";

/**
 * The app-level crash net. A render error inside any route otherwise blanks
 * the whole page with only a console stack to explain it; this keeps the
 * chrome alive, says what happened in plain words, and offers the two
 * recoveries that actually work — retry the render, or bail to the shelf.
 */
export class ErrorBoundary extends Component<
  { children: ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("render error", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="flex min-h-[60vh] flex-col items-center justify-center gap-3 px-6 text-center">
        <p className="font-display text-sm uppercase tracking-widest text-ink-300">
          Something broke
        </p>
        <p className="max-w-md text-sm leading-relaxed text-ink-500">
          {this.state.error.message || "An unexpected error occurred while drawing this page."}{" "}
          The rest of the app is fine — retry, or head back to the library.
        </p>
        <div className="mt-2 flex gap-2">
          <button
            type="button"
            onClick={() => this.setState({ error: null })}
            className="rounded-lg bg-fill-active px-4 py-2 text-sm text-ink-100 transition-colors hover:bg-fill-active"
          >
            Try again
          </button>
          <a
            href="/"
            className="rounded-lg px-4 py-2 text-sm text-ink-400 transition-colors hover:text-ink-100"
          >
            Home
          </a>
        </div>
      </div>
    );
  }
}
