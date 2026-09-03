import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { Field } from "./LoginPage";
import { StatsStrip } from "@/components/StatsStrip";
import { SteamImportDialog } from "@/components/SteamImportDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Panel } from "@/components/ui/primitives";
import { useArena } from "@/hooks/useArena";
import { useAuth } from "@/hooks/useAuth";
import {
  FAMILIES,
  THEMES,
  TIER_LABEL,
  themesInFamily,
  useTheme,
  type Theme,
  type ThemeFamily,
} from "@/hooks/useTheme";
import { api } from "@/lib/api";
import { ARENAS, ARENA_LABELS, type Arena } from "@/lib/arena";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";

export function SettingsPage() {
  const { user, logout } = useAuth();
  const { linked, setLinked } = useTheme();
  const { arena } = useArena();
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
            Ways to dress the app. Cover art, status colours and your accents stay the same in all
            of them — only the chrome changes.
          </p>

          <label className="mb-5 flex cursor-pointer items-start gap-2.5">
            <input
              type="checkbox"
              checked={linked}
              onChange={(event) => setLinked(event.target.checked)}
              className="mt-0.5 size-4 shrink-0 accent-brand-500"
            />
            <span className="min-w-0">
              <span className="block text-xs font-semibold text-ink-200">
                Use the same theme in both arenas
              </span>
              <span className="mt-0.5 block text-xs leading-relaxed text-ink-500">
                Turn this off to keep the cabinet for games and something quieter for books. The
                theme then changes with the arena you are in.
              </span>
            </span>
          </label>

          {linked ? (
            <ArenaTheme arena={arena} />
          ) : (
            <div className="grid gap-6 border-t border-line pt-5 sm:grid-cols-2">
              {ARENAS.map((key) => (
                <ArenaTheme key={key} arena={key} heading />
              ))}
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
 * One arena's theme choice: pick a family, then a backdrop if that family has
 * any.
 *
 * Rendered once when the arenas are linked and twice when they are not, which
 * is why it takes the arena rather than reading the active one. Picking the
 * books theme while standing in the games arena is the normal case with the
 * link off — the swatches are what make that legible, since the page around
 * you is wearing the *other* arena's theme the whole time.
 */
function ArenaTheme({ arena, heading = false }: { arena: Arena; heading?: boolean }) {
  const { slots, setTheme, setFamily } = useTheme();
  const theme: Theme = slots[arena];
  const family = THEMES[theme].family;
  const tier = TIER_LABEL[family];

  return (
    // Grouped and labelled when there are two of these on the page: without it
    // the second "Arcade" button is just a second unexplained Arcade button.
    <div role={heading ? "group" : undefined} aria-label={heading ? ARENA_LABELS[arena] : undefined}>
      {heading && (
        <h3 className="mb-3 flex items-center gap-1.5 text-xs font-semibold text-ink-200">
          <Gi name={arena === "books" ? "book-pile" : "gamepad"} className="size-3.5" />
          {ARENA_LABELS[arena]}
        </h3>
      )}

      <div className={cn("grid gap-3", !heading && "sm:grid-cols-2")}>
        {(Object.keys(FAMILIES) as ThemeFamily[]).map((key) => (
          <button
            key={key}
            type="button"
            onClick={() => setFamily(key, arena)}
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

      {/* A family with more than one theme gets a second row, and names it
          itself: the arcade's themes are its backdrops (its chrome is
          recoloured from the art), the library's are two grounds to read on. */}
      {tier && (
        <div className="mt-5 border-t border-line pt-4">
          <h4 className="mb-1 text-xs font-semibold text-ink-200">{tier.title}</h4>
          <p className="mb-3 text-xs text-ink-500">{tier.blurb}</p>
          <div className={cn("grid grid-cols-2 gap-2", !heading && "sm:grid-cols-5")}>
            {themesInFamily(family).map((key) => (
              <button
                key={key}
                type="button"
                onClick={() => setTheme(key, arena)}
                aria-pressed={theme === key}
                className={cn(
                  "flex flex-col items-center gap-1 p-3 text-center transition-colors focus-visible:focus-ring",
                  theme === key ? "f-panel-active" : "f-panel",
                )}
              >
                <span className="font-display text-[10px] uppercase tracking-wider text-ink-100">
                  {THEMES[key].label}
                </span>
                <span className="text-[10px] leading-snug text-ink-400">{THEMES[key].note}</span>
              </button>
            ))}
          </div>
        </div>
      )}
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
interface SwatchSpec {
  /** Outer surface, its radius, and the hairline or bevel around it. */
  bg: string;
  edge: string;
  radius: number;
  /** The panel sitting on it. */
  panelBg: string;
  panelEdge: string;
  panelRadius: number;
  /** The primary button. */
  btnBg: string;
  btnFg: string;
  btnRadius: number;
  font?: string;
  caps?: boolean;
  tracking?: string;
}

const SWATCHES: Record<ThemeFamily, SwatchSpec> = {
  flat: {
    bg: "#0c0c10",
    edge: "inset 0 0 0 1px rgba(255,255,255,0.07)",
    radius: 12,
    panelBg: "#17171f",
    panelEdge: "inset 0 0 0 1px rgba(255,255,255,0.06)",
    panelRadius: 8,
    btnBg: "#7c3aed",
    btnFg: "#fff",
    btnRadius: 8,
  },
  pixel: {
    bg: "#19101b",
    edge: "inset 0 0 0 2px #2b1c2e",
    radius: 2,
    panelBg: "#211623",
    panelEdge: "inset 0 0 0 2px #39253d",
    panelRadius: 0,
    btnBg: "#ffd071",
    btnFg: "#241b02",
    btnRadius: 0,
    font: '"Silkscreen", ui-monospace, monospace',
    caps: true,
    tracking: "0.06em",
  },
  library: {
    bg: "#0b0907",
    edge: "inset 0 0 0 1px rgba(255,233,205,0.09)",
    radius: 6,
    panelBg: "#130f0b",
    panelEdge: "inset 0 0 0 1px rgba(255,233,205,0.07)",
    panelRadius: 4,
    btnBg: "#f0a868",
    btnFg: "#1a1005",
    btnRadius: 4,
    font: "var(--f-serif)",
  },
};

/**
 * A miniature of what a family looks like: its surface, its edge, its
 * accent, its type.
 *
 * Deliberately hand-drawn from literal colours rather than the live
 * tokens. A token-driven preview would have to out-specify the family
 * rules already on <html>, and every card would quietly render as whichever
 * family you happened to be sitting in — a preview that lies about the thing
 * it is previewing is worse than no preview. The one exception is the serif
 * stack, which is a shared token rather than a family one.
 */
function ThemeSwatch({ family }: { family: ThemeFamily }) {
  const s = SWATCHES[family];
  return (
    <span
      aria-hidden="true"
      className="flex h-14 w-full items-center gap-2 overflow-hidden p-2"
      style={{ background: s.bg, borderRadius: s.radius, boxShadow: s.edge }}
    >
      <span
        className="flex-1 self-stretch"
        style={{
          background: s.panelBg,
          borderRadius: s.panelRadius,
          boxShadow: s.panelEdge,
        }}
      />
      <span
        className="flex h-8 items-center px-2.5 text-[10px] font-bold"
        style={{
          background: s.btnBg,
          color: s.btnFg,
          borderRadius: s.btnRadius,
          fontFamily: s.font ?? "inherit",
          textTransform: s.caps ? "uppercase" : "none",
          letterSpacing: s.tracking,
        }}
      >
        Aa
      </span>
    </span>
  );
}
