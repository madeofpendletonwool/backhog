import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { Field } from "./LoginPage";
import { StatsStrip } from "@/components/StatsStrip";
import { SteamImportDialog } from "@/components/SteamImportDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Panel } from "@/components/ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import {
  FAMILIES,
  THEMES,
  themesInFamily,
  useTheme,
  type ThemeFamily,
} from "@/hooks/useTheme";
import { api } from "@/lib/api";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";

export function SettingsPage() {
  const { user, logout } = useAuth();
  const { theme, family, setTheme, setFamily } = useTheme();
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
            Two ways to dress the app. Cover art, status colours and your accents stay the same in
            both — only the chrome changes.
          </p>

          <div className="grid gap-3 sm:grid-cols-2">
            {(Object.keys(FAMILIES) as ThemeFamily[]).map((key) => (
              <button
                key={key}
                type="button"
                onClick={() => setFamily(key)}
                aria-pressed={family === key}
                className={cn(
                  "flex flex-col items-start gap-2 p-4 text-left transition-colors focus-visible:focus-ring",
                  family === key ? "f-panel-active" : "f-panel",
                )}
              >
                <ThemeSwatch family={key} />
                <span className="flex items-center gap-1.5 text-sm font-semibold text-ink-100">
                  {FAMILIES[key].label}
                  {family === key && <Gi name="check-circle" className="size-3.5 text-hl-bright" />}
                </span>
                <span className="text-xs leading-relaxed text-ink-400">{FAMILIES[key].blurb}</span>
              </button>
            ))}
          </div>

          {/* Only the arcade family has backdrops: its chrome is recoloured
              from the backdrop art, so the two are one choice. */}
          {family === "pixel" && (
            <div className="mt-5 border-t border-line pt-4">
              <h3 className="mb-1 text-xs font-semibold text-ink-200">Backdrop</h3>
              <p className="mb-3 text-xs text-ink-500">
                Retints the whole cabinet to match the scene behind it.
              </p>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
                {themesInFamily("pixel").map((key) => (
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
                    <span className="font-display text-[10px] uppercase tracking-wider text-ink-100">
                      {THEMES[key].label}
                    </span>
                    <span className="text-[10px] leading-snug text-ink-400">
                      {THEMES[key].note}
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}
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

/**
 * A miniature of what a family looks like: its surface, its edge, its
 * accent, its type.
 *
 * Deliberately hand-drawn from literal colours rather than the live
 * tokens. A token-driven preview would have to out-specify the family
 * rules already on <html>, and the arcade card would quietly render as
 * Midnight whenever you were sitting in Midnight — a preview that lies
 * about the thing it is previewing is worse than no preview.
 */
function ThemeSwatch({ family }: { family: ThemeFamily }) {
  const pixel = family === "pixel";
  return (
    <span
      aria-hidden="true"
      className="flex h-14 w-full items-center gap-2 overflow-hidden p-2"
      style={{
        background: pixel ? "#19101b" : "#0c0c10",
        borderRadius: pixel ? 2 : 12,
        boxShadow: pixel ? "inset 0 0 0 2px #2b1c2e" : "inset 0 0 0 1px rgba(255,255,255,0.07)",
      }}
    >
      <span
        className="flex-1 self-stretch"
        style={{
          background: pixel ? "#211623" : "#17171f",
          borderRadius: pixel ? 0 : 8,
          boxShadow: pixel ? "inset 0 0 0 2px #39253d" : "inset 0 0 0 1px rgba(255,255,255,0.06)",
        }}
      />
      <span
        className="flex h-8 items-center px-2.5 text-[10px] font-bold"
        style={{
          background: pixel ? "#ffd071" : "#7c3aed",
          color: pixel ? "#241b02" : "#fff",
          borderRadius: pixel ? 0 : 8,
          fontFamily: pixel ? '"Silkscreen", ui-monospace, monospace' : "inherit",
          textTransform: pixel ? "uppercase" : "none",
          letterSpacing: pixel ? "0.06em" : undefined,
        }}
      >
        Aa
      </span>
    </span>
  );
}
