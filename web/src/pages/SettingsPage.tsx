import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { Field } from "./LoginPage";
import { StatsStrip } from "@/components/StatsStrip";
import { SteamImportDialog } from "@/components/SteamImportDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Panel } from "@/components/ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { THEMES, useTheme, type Theme } from "@/hooks/useTheme";
import { api } from "@/lib/api";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";

export function SettingsPage() {
  const { user, logout } = useAuth();
  const { theme, setTheme } = useTheme();
  const navigate = useNavigate();
  const [importOpen, setImportOpen] = useState(false);

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [status, setStatus] = useState<{ kind: "ok" | "error"; message: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const changePassword = async (event: React.FormEvent) => {
    event.preventDefault();
    setStatus(null);

    if (next.length < 8) {
      setStatus({ kind: "error", message: "New password must be at least 8 characters." });
      return;
    }

    setBusy(true);
    try {
      await api.changePassword(current, next);
      setCurrent("");
      setNext("");
      setStatus({
        kind: "ok",
        message: "Password updated. Any other devices have been signed out.",
      });
    } catch (error) {
      setStatus({ kind: "error", message: (error as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Settings</h1>
        <p className="mt-1 text-sm text-ink-400">Your account and a look at the damage.</p>
      </header>

      <div className="mb-6">
        <StatsStrip />
      </div>

      <div className="space-y-5">
        <Panel className="p-5">
          <h2 className="mb-1 text-sm font-semibold text-ink-200">Theme</h2>
          <p className="mb-4 text-xs text-ink-500">
            Picks a backdrop and retints the whole chrome to match — your accent colours stay put.
          </p>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
            {(Object.keys(THEMES) as Theme[]).map((key) => (
              <button
                key={key}
                type="button"
                onClick={() => setTheme(key)}
                aria-pressed={theme === key}
                className={cn(
                  "flex flex-col items-center gap-1 p-3 text-center transition-colors focus-visible:focus-ring",
                  theme === key ? "f-panel-active" : "f-panel",
                )}
              >
                <span className="font-pixel text-[10px] uppercase tracking-wider text-ink-100">
                  {THEMES[key].label}
                </span>
                <span className="text-[10px] leading-snug text-ink-400">{THEMES[key].note}</span>
              </button>
            ))}
          </div>
        </Panel>

        <Panel className="p-5">
          <h2 className="mb-4 text-sm font-semibold text-ink-200">Account</h2>
          <dl className="space-y-2.5 text-sm">
            <div className="flex justify-between">
              <dt className="text-ink-500">Username</dt>
              <dd className="text-ink-200">{user?.username}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-ink-500">Email</dt>
              <dd className="text-ink-200">{user?.email}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-ink-500">Member since</dt>
              <dd className="text-ink-200">{formatDate(user?.created_at ?? null)}</dd>
            </div>
          </dl>
        </Panel>

        <Panel className="p-5">
          <h2 className="mb-1 text-sm font-semibold text-ink-200">Change password</h2>
          <p className="mb-4 text-xs text-ink-500">
            Changing your password signs you out everywhere except this device.
          </p>

          <form onSubmit={changePassword} className="max-w-sm space-y-4">
            <Field label="Current password">
              <Input
                type="password"
                autoComplete="current-password"
                required
                value={current}
                onChange={(event) => setCurrent(event.target.value)}
              />
            </Field>

            <Field label="New password">
              <Input
                type="password"
                autoComplete="new-password"
                required
                minLength={8}
                value={next}
                onChange={(event) => setNext(event.target.value)}
              />
            </Field>

            {status && (
              <p
                role="alert"
                className={
                  status.kind === "ok"
                    ? "rounded-xl bg-emerald-500/10 px-3 py-2 text-sm text-emerald-300"
                    : "rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300"
                }
              >
                {status.message}
              </p>
            )}

            <Button type="submit" variant="primary" loading={busy}>
              Update password
            </Button>
          </form>
        </Panel>

        {/* The sidebar below lg carries these; on phones there is no sidebar,
            so Settings is their home. */}
        <Panel className="p-5 lg:hidden">
          <h2 className="mb-4 text-sm font-semibold text-ink-200">More</h2>
          <div className="flex flex-col gap-2">
            <Button onClick={() => setImportOpen(true)}>
              <Gi name="download" className="size-4" />
              Import from Steam
            </Button>
            <Button
              variant="ghost"
              className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
              onClick={async () => {
                await logout();
                navigate("/login");
              }}
            >
              <Gi name="log-out" className="size-4" />
              Sign out
            </Button>
          </div>
        </Panel>
      </div>

      <SteamImportDialog open={importOpen} onClose={() => setImportOpen(false)} />
    </div>
  );
}
