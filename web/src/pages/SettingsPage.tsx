import { useState } from "react";

import { Field } from "./LoginPage";
import { StatsStrip } from "@/components/StatsStrip";
import { Button, Input, Panel } from "@/components/ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { api } from "@/lib/api";
import { formatDate } from "@/lib/format";

export function SettingsPage() {
  const { user } = useAuth();

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
      </div>
    </div>
  );
}
