import { cn } from "@/lib/cn";
import { ArrowLeft, Calendar, Clock, Plus, Star, Trash2, Trophy } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

import { GameCover } from "@/components/GameCover";
import { StatusMenu } from "@/components/StatusMenu";
import { Button, Panel, Select, Skeleton } from "@/components/ui/primitives";
import { CreateListDialog } from "@/components/CreateListDialog";
import { SessionLog } from "@/components/SessionLog";
import { Dialog } from "@/components/ui/Dialog";
import { useDeleteEntry, useEntry, useUpdateEntry } from "@/hooks/useLibrary";
import { useEntryLists, useLists, useToggleListMembership } from "@/hooks/useLists";
import type { Entry } from "@/lib/types";
import { accentStyle, formatDate, formatDuration, relativeTime, releaseYear } from "@/lib/format";
import { coverUrl } from "@/lib/api";

export function GameDetailPage() {
  const { entryId } = useParams<{ entryId: string }>();
  const navigate = useNavigate();
  const { data: entry, isLoading } = useEntry(entryId);
  const update = useUpdateEntry();
  const remove = useDeleteEntry();

  const [notes, setNotes] = useState("");
  const [notesDirty, setNotesDirty] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Seed the notes field once the entry loads, without clobbering local edits.
  useEffect(() => {
    if (entry && !notesDirty) setNotes(entry.notes);
  }, [entry, notesDirty]);

  if (isLoading) return <DetailSkeleton />;

  if (!entry) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-20 text-center">
        <p className="text-ink-300">That game isn't in your library.</p>
        <Link to="/" className="mt-4 inline-block text-sm text-brand-400 hover:text-brand-300">
          Back to library
        </Link>
      </div>
    );
  }

  const { game } = entry;
  const hasCover = Boolean(game.cover_url);

  const saveNotes = () => {
    update.mutate(
      { id: entry.id, patch: { notes } },
      { onSuccess: () => setNotesDirty(false) },
    );
  };

  return (
    <div style={accentStyle(game)} className="animate-fade-rise">
      {/* Hero: the cover blown up and blurred behind its own artwork. */}
      <div className="relative isolate overflow-hidden">
        {hasCover && (
          <div
            className="absolute inset-0 -z-10 scale-110 bg-cover bg-center opacity-25 blur-3xl"
            style={{ backgroundImage: `url(${coverUrl(game.id)})` }}
            aria-hidden="true"
          />
        )}
        <div className="absolute inset-0 -z-10 bg-gradient-to-b from-ink-950/40 via-ink-950/85 to-ink-950" />

        <div className="mx-auto max-w-5xl px-4 pb-8 pt-6 sm:px-6 lg:px-8">
          <Link
            to="/"
            className="mb-6 inline-flex items-center gap-1.5 rounded-lg text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
          >
            <ArrowLeft className="size-4" />
            Library
          </Link>

          <div className="flex flex-col gap-6 sm:flex-row sm:items-end">
            <div className="w-36 shrink-0 sm:w-44">
              <GameCover game={game} className="shadow-2xl ring-1 ring-white/10" />
            </div>

            <div className="min-w-0 flex-1">
              <h1 className="text-3xl font-semibold tracking-tight text-white sm:text-4xl">
                {game.name}
              </h1>

              <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-ink-300">
                {releaseYear(game) && (
                  <span className="inline-flex items-center gap-1.5">
                    <Calendar className="size-4 text-ink-500" />
                    {releaseYear(game)}
                  </span>
                )}
                {game.time_to_beat_main && (
                  <span className="inline-flex items-center gap-1.5">
                    <Clock className="size-4 text-ink-500" />
                    {formatDuration(game.time_to_beat_main)} to beat
                  </span>
                )}
                {game.igdb_rating != null && (
                  <span className="inline-flex items-center gap-1.5">
                    <Trophy className="size-4 text-ink-500" />
                    {Math.round(game.igdb_rating)} on IGDB
                  </span>
                )}
              </div>

              {game.genres.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {game.genres.map((genre) => (
                    <span
                      key={genre.id}
                      className="rounded-full bg-white/[0.07] px-2.5 py-1 text-xs text-ink-300"
                    >
                      {genre.name}
                    </span>
                  ))}
                </div>
              )}

              <div className="mt-5 max-w-md">
                <StatusMenu entry={entry} size="md" />
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="mx-auto grid max-w-5xl gap-5 px-4 pb-16 sm:px-6 lg:grid-cols-3 lg:px-8">
        <div className="space-y-5 lg:col-span-2">
          {game.summary && (
            <Panel className="p-5">
              <h2 className="mb-2.5 text-sm font-semibold text-ink-200">About</h2>
              <p className="text-sm leading-relaxed text-ink-400">{game.summary}</p>
            </Panel>
          )}

          <Panel className="p-5">
            <div className="mb-2.5 flex items-center justify-between">
              <h2 className="text-sm font-semibold text-ink-200">Notes</h2>
              {notesDirty && (
                <Button size="sm" variant="primary" loading={update.isPending} onClick={saveNotes}>
                  Save
                </Button>
              )}
            </div>
            <textarea
              value={notes}
              onChange={(event) => {
                setNotes(event.target.value);
                setNotesDirty(true);
              }}
              rows={4}
              placeholder="Where you left off, why you bounced off it, what to do next…"
              className="w-full resize-y rounded-xl border border-white/[0.07] bg-ink-850 p-3 text-sm text-ink-100 placeholder:text-ink-600 focus:border-brand-500/50 focus-visible:focus-ring"
            />
          </Panel>
        </div>

        <div className="space-y-5">
          <SessionLog entry={entry} />

          <Panel className="p-5">
            <h2 className="mb-3 text-sm font-semibold text-ink-200">Your rating</h2>
            <RatingPicker
              value={entry.user_rating}
              onChange={(rating) =>
                update.mutate({ id: entry.id, patch: { user_rating: rating } })
              }
            />
          </Panel>

          <Panel className="p-5">
            <h2 className="mb-3 text-sm font-semibold text-ink-200">Timeline</h2>
            <dl className="space-y-2.5 text-sm">
              <Row label="Added" value={`${formatDate(entry.created_at)}`} hint={relativeTime(entry.created_at)} />
              <Row label="Started" value={formatDate(entry.started_at)} hint={relativeTime(entry.started_at)} />
              <Row label="Finished" value={formatDate(entry.finished_at)} hint={relativeTime(entry.finished_at)} />
            </dl>
          </Panel>

          <ListMembership entry={entry} />

          {game.platforms.length > 0 && (
            <Panel className="p-5">
              <h2 className="mb-1 text-sm font-semibold text-ink-200">Platform</h2>
              <p className="mb-3 text-xs text-ink-500">Which one are you playing it on?</p>
              <Select
                value={entry.platform_id == null ? "" : String(entry.platform_id)}
                onChange={(event) =>
                  update.mutate({
                    id: entry.id,
                    patch: {
                      // An explicit null clears it; omitting the key would mean
                      // "leave unchanged" to the API.
                      platform_id: event.target.value === "" ? null : Number(event.target.value),
                    },
                  })
                }
              >
                <option value="">Not set</option>
                {game.platforms.map((platform) => (
                  <option key={platform.id} value={platform.id}>
                    {platform.name}
                  </option>
                ))}
              </Select>
            </Panel>
          )}

          <Button
            variant="ghost"
            className="w-full text-red-400 hover:bg-red-500/10 hover:text-red-300"
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 className="size-4" />
            Remove from library
          </Button>
        </div>
      </div>

      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)} label="Confirm removal">
        <h2 className="text-lg font-semibold text-ink-100">Remove {game.name}?</h2>
        <p className="mt-2 text-sm text-ink-400">
          This removes it from your library, along with your rating and notes. The game itself stays
          searchable, so you can add it again later.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
            Cancel
          </Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() => remove.mutate(entry.id, { onSuccess: () => navigate("/") })}
          >
            Remove
          </Button>
        </div>
      </Dialog>
    </div>
  );
}

/**
 * Manual-list membership for this game. Smart lists are excluded: their
 * contents are decided by rules, so a checkbox here would be a lie.
 */
function ListMembership({ entry }: { entry: Entry }) {
  const { data: listData } = useLists();
  const { data: membership } = useEntryLists(entry.id);
  const toggle = useToggleListMembership(entry.id);
  const [creating, setCreating] = useState(false);

  const manualLists = listData?.lists.filter((list) => list.kind === "manual") ?? [];
  const memberOf = new Set(membership?.list_ids ?? []);

  return (
    <Panel className="p-5">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-ink-200">Lists</h2>
        <Button size="sm" variant="ghost" onClick={() => setCreating(true)}>
          <Plus className="size-3.5" />
          New
        </Button>
      </div>

      {manualLists.length === 0 ? (
        <p className="text-xs leading-relaxed text-ink-500">
          No manual lists yet. Create one to group games however you like.
        </p>
      ) : (
        <div className="space-y-0.5">
          {manualLists.map((list) => {
            const member = memberOf.has(list.id);
            return (
              <label
                key={list.id}
                className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-white/[0.05]"
              >
                <input
                  type="checkbox"
                  checked={member}
                  disabled={toggle.isPending}
                  onChange={() => toggle.mutate({ listId: list.id, member })}
                  className="size-4 shrink-0 accent-brand-500"
                />
                <span className="min-w-0 flex-1 truncate text-sm text-ink-200">{list.name}</span>
                <span className="shrink-0 text-[11px] tabular-nums text-ink-600">{list.count}</span>
              </label>
            );
          })}
        </div>
      )}

      <CreateListDialog open={creating} onClose={() => setCreating(false)} />
    </Panel>
  );
}

function Row({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <dt className="text-ink-500">{label}</dt>
      <dd className="text-right">
        <span className="text-ink-200">{value}</span>
        {hint && value !== "—" && <span className="ml-1.5 text-xs text-ink-600">{hint}</span>}
      </dd>
    </div>
  );
}

/** 1–10 rating. Clicking the active score clears it. */
function RatingPicker({
  value,
  onChange,
}: {
  value: number | null;
  onChange: (rating: number | null) => void;
}) {
  const [hovered, setHovered] = useState<number | null>(null);
  const shown = hovered ?? value ?? 0;

  return (
    <div>
      <div className="flex gap-1" onMouseLeave={() => setHovered(null)}>
        {Array.from({ length: 10 }, (_, index) => index + 1).map((score) => (
          <button
            key={score}
            aria-label={`Rate ${score} out of 10`}
            onMouseEnter={() => setHovered(score)}
            onClick={() => onChange(value === score ? null : score)}
            className={cn(
              "flex h-8 flex-1 items-center justify-center rounded-md text-xs font-semibold transition-colors focus-visible:focus-ring",
              score <= shown
                ? "bg-amber-400/90 text-ink-950"
                : "bg-ink-800 text-ink-600 hover:bg-ink-750",
            )}
          >
            {score}
          </button>
        ))}
      </div>
      <p className="mt-2 flex items-center gap-1.5 text-xs text-ink-500">
        {value != null ? (
          <>
            <Star className="size-3 fill-amber-300 text-amber-300" />
            You rated this {value}/10 — click again to clear
          </>
        ) : (
          "Not rated yet"
        )}
      </p>
    </div>
  );
}

function DetailSkeleton() {
  return (
    <div className="mx-auto max-w-5xl px-4 py-8 sm:px-6 lg:px-8">
      <div className="flex flex-col gap-6 sm:flex-row">
        <Skeleton className="h-56 w-36 shrink-0 sm:w-44" />
        <div className="flex-1 space-y-3">
          <Skeleton className="h-10 w-2/3" />
          <Skeleton className="h-4 w-1/3" />
          <Skeleton className="h-12 w-full max-w-md" />
        </div>
      </div>
      <div className="mt-8 grid gap-5 lg:grid-cols-3">
        <Skeleton className="h-40 lg:col-span-2" />
        <Skeleton className="h-40" />
      </div>
    </div>
  );
}
