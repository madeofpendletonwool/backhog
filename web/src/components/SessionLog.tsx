import { cn } from "@/lib/cn";
import { Clock, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

import { Button, Input, Panel } from "./ui/primitives";
import { useAddSession, useDeleteSession, useSessions } from "@/hooks/useSessions";
import { formatHours } from "@/lib/format";
import type { Entry } from "@/lib/types";

/** Common session lengths, so the usual case is one tap. */
const PRESETS = [15, 30, 45, 60, 90, 120, 180];

function formatMinutes(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (hours === 0) return `${rest}m`;
  if (rest === 0) return `${hours}h`;
  return `${hours}h ${rest}m`;
}

/** Renders a stored 2026-07-20 without the timezone shifts of `new Date()`. */
function formatPlayedOn(isoDate: string): string {
  const [year, month, day] = isoDate.split("-").map(Number);
  if (!year || !month || !day) return isoDate;
  return new Date(year, month - 1, day).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function today(): string {
  const now = new Date();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${day}`;
}

/**
 * Manual playtime log.
 *
 * There's no running timer on purpose: a timer measures how long the game was
 * open, which is not how long you played it — you get up, deal with something,
 * come back an hour later. Logging a rough figure after the fact is less
 * precise and much more honest.
 */
export function SessionLog({ entry }: { entry: Entry }) {
  const { data } = useSessions(entry.id);
  const addSession = useAddSession(entry.id);
  const deleteSession = useDeleteSession(entry.id);

  const [open, setOpen] = useState(false);
  const [minutes, setMinutes] = useState(60);
  const [playedOn, setPlayedOn] = useState(today);
  const [note, setNote] = useState("");

  const sessions = data?.sessions ?? [];
  const totalMinutes = sessions.reduce((sum, session) => sum + session.minutes, 0);
  const estimate = entry.game.time_to_beat_main;

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    addSession.mutate(
      { minutes, played_on: playedOn, note: note.trim() },
      {
        onSuccess: () => {
          setNote("");
          setMinutes(60);
          setPlayedOn(today());
          setOpen(false);
        },
      },
    );
  };

  return (
    <Panel className="p-5">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-ink-200">Playtime</h2>
          <p className="mt-0.5 text-xs text-ink-500">
            {totalMinutes > 0 ? (
              <>
                <span className="text-ink-300">{formatMinutes(totalMinutes)}</span> logged
                {estimate ? ` · ~${formatHours(estimate / 3600)} to beat` : ""}
              </>
            ) : (
              "Nothing logged yet"
            )}
          </p>
        </div>
        {!open && (
          <Button size="sm" onClick={() => setOpen(true)}>
            <Plus className="size-3.5" />
            Log
          </Button>
        )}
      </div>

      {/* Progress against the estimate, when we have one to compare to. */}
      {totalMinutes > 0 && estimate ? (
        <div
          className="mb-4 h-1.5 overflow-hidden rounded-full bg-ink-800"
          role="progressbar"
          aria-valuenow={Math.min(Math.round((totalMinutes * 60 * 100) / estimate), 100)}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label="Progress against estimated length"
        >
          <div
            className="h-full rounded-full bg-gradient-to-r from-cyan-400 to-emerald-400 transition-[width] duration-700 ease-[var(--ease-spring)]"
            style={{ width: `${Math.min((totalMinutes * 60 * 100) / estimate, 100)}%` }}
          />
        </div>
      ) : null}

      {open && (
        <form onSubmit={submit} className="mb-4 space-y-3 rounded-xl bg-ink-850/60 p-3">
          <div>
            <span className="mb-1.5 block text-xs font-medium text-ink-400">How long?</span>
            <div className="flex flex-wrap gap-1.5">
              {PRESETS.map((preset) => (
                <button
                  key={preset}
                  type="button"
                  onClick={() => setMinutes(preset)}
                  className={cn(
                    "rounded-lg px-2.5 py-1.5 text-xs font-medium transition-colors",
                    minutes === preset
                      ? "bg-brand-600 text-white"
                      : "bg-ink-800 text-ink-400 hover:text-ink-100",
                  )}
                >
                  {formatMinutes(preset)}
                </button>
              ))}
            </div>
            <div className="mt-2 flex items-center gap-2">
              <Input
                type="number"
                min={1}
                max={1440}
                value={minutes}
                onChange={(event) => setMinutes(Number(event.target.value))}
                className="h-9 w-24 text-xs"
                aria-label="Minutes played"
              />
              <span className="text-xs text-ink-500">minutes</span>
            </div>
          </div>

          <div>
            <span className="mb-1.5 block text-xs font-medium text-ink-400">When?</span>
            <Input
              type="date"
              value={playedOn}
              max={today()}
              onChange={(event) => setPlayedOn(event.target.value)}
              className="h-9 text-xs"
            />
          </div>

          <Input
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder="Where did you get to? (optional)"
            className="h-9 text-xs"
          />

          {addSession.isError && (
            <p role="alert" className="text-xs text-red-400">
              {(addSession.error as Error).message}
            </p>
          )}

          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" size="sm" variant="primary" loading={addSession.isPending}>
              Log session
            </Button>
          </div>
        </form>
      )}

      {sessions.length > 0 && (
        <ul className="space-y-0.5">
          {sessions.map((session) => (
            <li
              key={session.id}
              className="group flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-white/[0.04]"
            >
              <Clock className="size-3.5 shrink-0 text-ink-600" />
              <span className="shrink-0 tabular-nums text-ink-200">
                {formatMinutes(session.minutes)}
              </span>
              <span className="shrink-0 text-xs text-ink-500">
                {formatPlayedOn(session.played_on)}
              </span>
              {session.note && (
                <span className="min-w-0 flex-1 truncate text-xs text-ink-600">{session.note}</span>
              )}
              <button
                onClick={() => deleteSession.mutate(session.id)}
                aria-label="Delete session"
                className="ml-auto shrink-0 rounded p-1 text-ink-700 opacity-0 transition-opacity hover:text-red-400 focus-visible:opacity-100 group-hover:opacity-100"
              >
                <Trash2 className="size-3.5" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
