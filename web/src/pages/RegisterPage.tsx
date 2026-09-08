import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { AuthShell } from "./AuthShell";
import { Field } from "./LoginPage";
import { Button, Input, Spinner } from "@/components/ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { useAuthConfig } from "@/hooks/useAuthConfig";
import { ROLE_COPY } from "@/lib/types";

export function RegisterPage() {
  const { register } = useAuth();
  const [params] = useSearchParams();
  const token = params.get("invite") ?? "";
  const { data: config, isLoading } = useAuthConfig(token || undefined);

  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // The invite's email pre-fills the form once, and stays editable: it is
  // who the link was cut for, not a constraint the server enforces.
  const [prefilled, setPrefilled] = useState(false);
  if (!prefilled && config?.invite?.email) {
    setPrefilled(true);
    setEmail(config.invite.email);
  }

  const onSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setError("");

    if (password.length < 8) {
      setError("Password must be at least 8 characters.");
      return;
    }

    setBusy(true);
    try {
      await register(email, username, password, token || undefined);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const signInLink = (
    <>
      Already have an account?{" "}
      <Link to="/login" className="font-medium text-brand-400 hover:text-brand-300">
        Sign in
      </Link>
    </>
  );

  if (isLoading) {
    return (
      <AuthShell title="Start hogging" subtitle="One moment." footer={signInLink}>
        <div className="flex justify-center py-6">
          <Spinner className="size-6" />
        </div>
      </AuthShell>
    );
  }

  // The door is shut and this caller has no key. Say so plainly rather than
  // showing a form whose only possible outcome is a 403.
  const invited = !!config?.invite;
  const open = config?.registration_enabled || config?.setup;
  if (!invited && !open) {
    return (
      <AuthShell
        title="Invite only"
        subtitle="This Backhog is not taking sign-ups."
        footer={signInLink}
      >
        <p className="rounded-xl bg-ink-900/40 px-3 py-2.5 text-sm leading-relaxed text-ink-400">
          Accounts here are made by invitation. Ask whoever runs this server for a sign-up
          link and open it — the link brings you back to this page with a key.
        </p>
      </AuthShell>
    );
  }

  // A token that came back unresolved is spent, expired or was never real.
  // The server does not distinguish the three, and neither does this.
  const staleToken = !!token && !invited;

  const subtitle = config?.setup
    ? "Nobody has an account here yet, so this one runs the place."
    : invited
      ? `${config?.invite?.invited_by || "Someone"} invited you.`
      : "Track the games you own, and the ones you'll get to eventually.";

  return (
    <AuthShell title="Start hogging" subtitle={subtitle} footer={signInLink}>
      {staleToken && (
        <p className="mb-4 rounded-xl bg-amber-500/10 px-3 py-2.5 text-sm leading-relaxed text-amber-300">
          That sign-up link is no longer valid — it may have been used already, withdrawn, or
          simply run out. Ask for a fresh one.
        </p>
      )}

      {invited && config?.invite && (
        <div className="mb-4 rounded-xl bg-ink-900/40 px-3 py-2.5">
          <p className="text-sm font-medium text-ink-200">
            You are joining as a {ROLE_COPY[config.invite.role].label.toLowerCase()}.
          </p>
          <p className="mt-0.5 text-xs leading-relaxed text-ink-500">
            {ROLE_COPY[config.invite.role].blurb}
          </p>
        </div>
      )}

      {config?.setup && (
        <p className="mb-4 rounded-xl bg-ink-900/40 px-3 py-2.5 text-xs leading-relaxed text-ink-500">
          The first account on a server becomes its administrator: it manages the other
          accounts, the invites and the settings.
        </p>
      )}

      <form onSubmit={onSubmit} className="space-y-4">
        <Field label="Email">
          <Input
            type="email"
            autoComplete="email"
            required
            autoFocus
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="you@example.com"
          />
        </Field>

        <Field label="Username">
          <Input
            autoComplete="username"
            required
            minLength={2}
            maxLength={32}
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            placeholder="backlogslayer"
          />
        </Field>

        <Field label="Password">
          <Input
            type="password"
            autoComplete="new-password"
            required
            minLength={8}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder="At least 8 characters"
          />
        </Field>

        {error && (
          <p role="alert" className="rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
            {error}
          </p>
        )}

        <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy}>
          Create account
        </Button>
      </form>
    </AuthShell>
  );
}
