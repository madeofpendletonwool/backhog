import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Navigate } from "react-router-dom";

import { Field } from "./LoginPage";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Panel, Select, Spinner } from "@/components/ui/primitives";
import { useAuth } from "@/hooks/useAuth";
import { api } from "@/lib/api";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";
import { ROLES, ROLE_COPY, type AdminUser, type Invite, type Role } from "@/lib/types";

/**
 * The account panel: who has an account here, what each of them may do, how
 * new ones get made, and whether anyone may make their own.
 *
 * It is one page rather than three because the three questions are asked
 * together — you close self-service registration *because* you are about to
 * invite someone, and you pick their role in the same breath.
 */
export function AdminPage() {
  const { isAdmin, loading } = useAuth();

  if (loading) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center">
        <Spinner className="size-6" />
      </div>
    );
  }
  // The routes enforce this too; this only keeps a stray link from landing
  // someone on a page of failed requests.
  if (!isAdmin) return <Navigate to="/settings" replace />;

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Accounts</h1>
        <p className="mt-1 text-sm text-ink-400">
          Who is on this server, what they can touch, and how anyone else gets in.
        </p>
      </header>

      <div className="space-y-5">
        <RegistrationPanel />
        <InvitesPanel />
        <UsersPanel />
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */

function RegistrationPanel() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["server-settings"],
    queryFn: api.serverSettings,
  });
  const [note, setNote] = useState("");

  const save = useMutation({
    mutationFn: api.saveServerSettings,
    onSuccess: (settings) => {
      queryClient.setQueryData(["server-settings"], settings);
      // The login page reads this too, and its answer just changed.
      void queryClient.invalidateQueries({ queryKey: ["auth-config"] });
      setNote("Saved.");
    },
    onError: (error: Error) => setNote(error.message),
  });

  if (isLoading || !data) {
    return (
      <Panel className="p-5">
        <Spinner className="size-5" />
      </Panel>
    );
  }

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Sign-ups</h2>
      <p className="mb-4 text-xs leading-relaxed text-ink-500">
        With sign-ups closed, the only way in is an invite link. Closing the door never
        locks anyone out: existing accounts and outstanding invites keep working.
      </p>

      <label className="mb-4 flex cursor-pointer items-start gap-2.5">
        <input
          type="checkbox"
          checked={data.registration_enabled}
          onChange={(event) =>
            save.mutate({ ...data, registration_enabled: event.target.checked })
          }
          className="mt-0.5 size-4 shrink-0 accent-brand-500"
        />
        <span className="min-w-0">
          <span className="block text-xs font-semibold text-ink-200">
            Anyone can create their own account
          </span>
          <span className="mt-0.5 block text-xs leading-relaxed text-ink-500">
            Leave this off unless the server is on a network you trust.
          </span>
        </span>
      </label>

      <div className="max-w-sm">
        <Field label="Role for self-made accounts">
          <Select
            value={data.default_role}
            disabled={!data.registration_enabled}
            onChange={(event) =>
              save.mutate({ ...data, default_role: event.target.value as Role })
            }
          >
            {ROLES.map((role) => (
              <option key={role} value={role}>
                {ROLE_COPY[role].label}
              </option>
            ))}
          </Select>
        </Field>
        <p className="mt-1.5 text-xs leading-relaxed text-ink-500">
          {ROLE_COPY[data.default_role].blurb}
        </p>
      </div>

      {note && <p className="mt-3 text-xs text-ink-400">{note}</p>}
    </Panel>
  );
}

/* ------------------------------------------------------------------ */

function InvitesPanel() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["invites"], queryFn: api.invites });

  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("reader");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  // The token exists exactly once, in the response that made it. Held here
  // until the admin has copied it, because there is no second chance: the
  // server stores only a hash.
  const [fresh, setFresh] = useState<Invite | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["invites"] });

  const create = useMutation({
    mutationFn: () => api.createInvite({ email: email.trim(), role, note: note.trim() }),
    onSuccess: (invite) => {
      setFresh(invite);
      setEmail("");
      setNote("");
      setError("");
      void refresh();
    },
    onError: (err: Error) => setError(err.message),
  });

  const revoke = useMutation({
    mutationFn: api.revokeInvite,
    onSuccess: () => void refresh(),
    onError: (err: Error) => setError(err.message),
  });

  const remove = useMutation({
    mutationFn: api.deleteInvite,
    onSuccess: () => void refresh(),
    onError: (err: Error) => setError(err.message),
  });

  const invites = data?.invites ?? [];

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Invites</h2>
      <p className="mb-4 text-xs leading-relaxed text-ink-500">
        A single-use link that creates one account with the role you pick. The link is
        shown once, when you make it — after that only its fingerprint is stored, so a
        lost one is replaced rather than looked up.
      </p>

      <form
        className="mb-4 grid gap-3 sm:grid-cols-[1fr_auto_auto] sm:items-end"
        onSubmit={(event) => {
          event.preventDefault();
          create.mutate();
        }}
      >
        <Field label="Email (optional)">
          <Input
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="friend@example.com"
          />
        </Field>
        <Field label="Role">
          <Select value={role} onChange={(event) => setRole(event.target.value as Role)}>
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {ROLE_COPY[r].label}
              </option>
            ))}
          </Select>
        </Field>
        <Button type="submit" variant="primary" loading={create.isPending}>
          <Gi name="gift" className="size-4" />
          Create link
        </Button>
      </form>

      <p className="-mt-2 mb-4 text-xs leading-relaxed text-ink-500">{ROLE_COPY[role].blurb}</p>

      {fresh?.token && <FreshInvite invite={fresh} onDone={() => setFresh(null)} />}

      {error && (
        <p role="alert" className="mb-3 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
          {error}
        </p>
      )}

      {isLoading ? (
        <Spinner className="size-5" />
      ) : invites.length === 0 ? (
        <p className="text-xs text-ink-500">No invites yet.</p>
      ) : (
        <ul className="divide-y divide-line">
          {invites.map((invite) => (
            <li key={invite.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5">
              <InviteStatusChip status={invite.status} />
              <span className="min-w-0 flex-1 truncate text-sm text-ink-200">
                {invite.email || <span className="text-ink-500">anyone with the link</span>}
                <span className="ml-2 text-xs text-ink-500">
                  {ROLE_COPY[invite.role].label}
                  {invite.note && ` · ${invite.note}`}
                </span>
              </span>
              <span className="text-xs text-ink-500">
                {invite.status === "accepted"
                  ? `used by ${invite.accepted_by_username ?? "someone"}`
                  : `expires ${formatDate(invite.expires_at)}`}
              </span>
              {invite.status === "pending" ? (
                <Button
                  size="sm"
                  variant="ghost"
                  loading={revoke.isPending}
                  onClick={() => revoke.mutate(invite.id)}
                >
                  Revoke
                </Button>
              ) : (
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-ink-500"
                  onClick={() => remove.mutate(invite.id)}
                >
                  <Gi name="trash" className="size-3.5" label="Remove" />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

/** The one showing of a new invite's link, with a copy button. */
function FreshInvite({ invite, onDone }: { invite: Invite; onDone: () => void }) {
  const [copied, setCopied] = useState(false);
  const url = `${window.location.origin}/register?invite=${invite.token}`;

  return (
    <div className="mb-4 rounded-xl bg-emerald-500/10 p-3">
      <p className="mb-2 text-xs font-semibold text-emerald-300">
        Copy this link now — it is not shown again.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-lg bg-ink-950/50 px-2.5 py-1.5 text-xs text-ink-300">
          {url}
        </code>
        <Button
          size="sm"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(url);
              setCopied(true);
            } catch {
              // Clipboard access can be refused (an insecure origin, a
              // locked-down browser). The link is on screen and selectable
              // either way, so this is a convenience, not the mechanism.
              setCopied(false);
            }
          }}
        >
          {copied ? "Copied" : "Copy"}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          Done
        </Button>
      </div>
    </div>
  );
}

function InviteStatusChip({ status }: { status: Invite["status"] }) {
  const tone =
    status === "pending"
      ? "bg-emerald-500/15 text-emerald-300"
      : status === "accepted"
        ? "bg-ink-700/50 text-ink-400"
        : "bg-amber-500/10 text-amber-300";
  return (
    <span className={cn("rounded-md px-1.5 py-0.5 text-[11px] font-medium capitalize", tone)}>
      {status}
    </span>
  );
}

/* ------------------------------------------------------------------ */

function UsersPanel() {
  const queryClient = useQueryClient();
  const { user: me } = useAuth();
  const { data, isLoading } = useQuery({ queryKey: ["admin-users"], queryFn: api.adminUsers });
  const [error, setError] = useState("");

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["admin-users"] });
  const onError = (err: Error) => setError(err.message);

  const update = useMutation({
    mutationFn: ({ id, changes }: { id: string; changes: { role?: Role; disabled?: boolean } }) =>
      api.updateUser(id, changes),
    onSuccess: () => {
      setError("");
      void refresh();
    },
    onError,
  });

  const remove = useMutation({
    mutationFn: api.deleteUser,
    onSuccess: () => {
      setError("");
      void refresh();
    },
    onError,
  });

  const reset = useMutation({
    mutationFn: ({ id, password }: { id: string; password: string }) =>
      api.resetUserPassword(id, password),
    onSuccess: () => setError(""),
    onError,
  });

  if (isLoading) {
    return (
      <Panel className="p-5">
        <Spinner className="size-5" />
      </Panel>
    );
  }

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">People</h2>
      <p className="mb-4 text-xs leading-relaxed text-ink-500">
        A <strong className="font-semibold text-ink-300">reader</strong> uses the whole app on
        their own shelf but cannot attach or detach library files, move a book&rsquo;s primary
        text, run the scanner or start an alignment. That is the account to give a friend.
      </p>

      {error && (
        <p role="alert" className="mb-3 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
          {error}
        </p>
      )}

      <ul className="divide-y divide-line">
        {(data?.users ?? []).map((account) => (
          <UserRow
            key={account.id}
            account={account}
            isMe={account.id === me?.id}
            busy={update.isPending || remove.isPending}
            onRole={(role) => update.mutate({ id: account.id, changes: { role } })}
            onDisabled={(disabled) => update.mutate({ id: account.id, changes: { disabled } })}
            onDelete={() => remove.mutate(account.id)}
            onReset={(password) => reset.mutate({ id: account.id, password })}
            resetDone={reset.isSuccess}
          />
        ))}
      </ul>
    </Panel>
  );
}

function UserRow({
  account,
  isMe,
  busy,
  onRole,
  onDisabled,
  onDelete,
  onReset,
  resetDone,
}: {
  account: AdminUser;
  isMe: boolean;
  busy: boolean;
  onRole: (role: Role) => void;
  onDisabled: (disabled: boolean) => void;
  onDelete: () => void;
  onReset: (password: string) => void;
  resetDone: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [confirming, setConfirming] = useState(false);
  const disabled = !!account.disabled_at;

  return (
    <li className="py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-ink-200">
            {account.username}
            {isMe && <span className="ml-2 text-xs font-normal text-ink-500">you</span>}
            {disabled && (
              <span className="ml-2 rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-300">
                disabled
              </span>
            )}
          </p>
          <p className="truncate text-xs text-ink-500">
            {account.email} · {account.entry_count} in library
            {account.shared_count > 0 && ` · sharing ${account.shared_count}`}
            {account.received_count > 0 && ` · borrowing ${account.received_count}`}
          </p>
        </div>

        {/* Your own row is read-only: the last-administrator guard already
            stops the dangerous half of self-editing, and the harmless half
            — demoting yourself and watching this page vanish — is just
            confusing. */}
        {isMe ? (
          <span className="text-xs text-ink-500">{ROLE_COPY[account.role].label}</span>
        ) : (
          <>
            <Select
              value={account.role}
              disabled={busy}
              className="w-auto"
              onChange={(event) => onRole(event.target.value as Role)}
            >
              {ROLES.map((role) => (
                <option key={role} value={role}>
                  {ROLE_COPY[role].label}
                </option>
              ))}
            </Select>
            <Button size="sm" variant="ghost" onClick={() => setOpen(!open)}>
              <Gi name={open ? "chevron-up" : "chevron-down"} className="size-3.5" label="More" />
            </Button>
          </>
        )}
      </div>

      {open && !isMe && (
        <div className="mt-3 space-y-3 rounded-xl bg-ink-900/40 p-3">
          <div className="flex flex-wrap items-end gap-2">
            <div className="min-w-0 flex-1">
              <Field label="Set a new password for them">
                <Input
                  type="password"
                  autoComplete="new-password"
                  minLength={8}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="At least 8 characters"
                />
              </Field>
            </div>
            <Button
              size="sm"
              disabled={password.length < 8}
              onClick={() => {
                onReset(password);
                setPassword("");
              }}
            >
              {resetDone ? "Set" : "Set password"}
            </Button>
          </div>
          <p className="text-xs leading-relaxed text-ink-500">
            Setting it signs them out of every device — a password they did not choose should
            not leave their old logins running.
          </p>

          <div className="flex flex-wrap gap-2 border-t border-line pt-3">
            <Button size="sm" variant="ghost" onClick={() => onDisabled(!disabled)}>
              <Gi name={disabled ? "check-circle" : "ban"} className="size-3.5" />
              {disabled ? "Restore access" : "Disable account"}
            </Button>
            {confirming ? (
              <>
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
                  onClick={onDelete}
                >
                  Delete {account.username} and their {account.entry_count} entries
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                  Cancel
                </Button>
              </>
            ) : (
              <Button
                size="sm"
                variant="ghost"
                className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
                onClick={() => setConfirming(true)}
              >
                <Gi name="trash" className="size-3.5" />
                Delete account
              </Button>
            )}
          </div>
          <p className="text-xs leading-relaxed text-ink-500">
            Disabling keeps everything and only stops them signing in. Deleting takes their
            library, progress and reading history with it, and cannot be undone — the files on
            the NAS are untouched either way.
          </p>
        </div>
      )}
    </li>
  );
}
