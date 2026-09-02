import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/cn";
import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

import { BookCover } from "@/components/BookCover";
import { ListMembership, ProjectMembership } from "@/components/EntryMembership";
import { StatusMenu } from "@/components/StatusMenu";
import { Dialog } from "@/components/ui/Dialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Panel, Skeleton } from "@/components/ui/primitives";
import { useBook, useBookEntry } from "@/hooks/useBooks";
import { useDeleteEntry, useUpdateEntry } from "@/hooks/useLibrary";
import { useAudioPlayer } from "@/hooks/useAudioPlayer";
import { api, bookCoverUrl } from "@/lib/api";
import {
  accentStyle,
  byline,
  editionISBN,
  editionLabel,
  formatDate,
  formatDuration,
  formatPages,
  publishYear,
  relativeTime,
} from "@/lib/format";
import { STATUSES, type Book, type BookEdition, type BookEntry } from "@/lib/types";

/**
 * One book, the mirror of GameDetailPage: the provider's dossier on the left,
 * what *you* have done with it on the right. The two pages share the status
 * control, the rating picker's shape, the timeline and both membership panels,
 * so moving between arenas never feels like moving between apps.
 */
export function BookDetailPage() {
  const { entryId } = useParams<{ entryId: string }>();
  const navigate = useNavigate();
  const { entry, isLoading } = useBookEntry(entryId);
  const update = useUpdateEntry();
  const remove = useDeleteEntry();

  // The library projection serves works lean; the printings come from the
  // work read, which is cached and shared with the add dialog.
  const { data: work } = useBook(entry?.book.id);

  const [notes, setNotes] = useState("");
  const [notesDirty, setNotesDirty] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (entry && !notesDirty) setNotes(entry.notes);
  }, [entry, notesDirty]);

  if (isLoading) return <DetailSkeleton />;

  // This is the books detail route; a game id simply is not this page's item.
  if (!entry) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-20 text-center">
        <p className="text-ink-300">That book isn't on your shelf.</p>
        <Link to="/books" className="mt-4 inline-block text-sm text-brand-400 hover:text-brand-300">
          Back to the shelf
        </Link>
      </div>
    );
  }

  const book = work ?? entry.book;
  const editions = book.editions ?? [];
  const hasCover = Boolean(book.cover_url);

  const saveNotes = () => {
    update.mutate({ id: entry.id, patch: { notes } }, { onSuccess: () => setNotesDirty(false) });
  };

  return (
    <div style={accentStyle(book)} className="animate-fade-rise">
      {/* Hero: the jacket blown up and blurred behind its own artwork. */}
      <div className="relative isolate overflow-hidden">
        {hasCover && (
          <div
            className="absolute inset-0 -z-10 scale-110 bg-cover bg-center opacity-25 blur-3xl"
            style={{ backgroundImage: `url(${bookCoverUrl(book.id)})` }}
            aria-hidden="true"
          />
        )}
        <div className="absolute inset-0 -z-10 bg-gradient-to-b from-ink-950/40 via-ink-950/85 to-ink-950" />

        <div className="mx-auto max-w-5xl px-4 pb-8 pt-6 sm:px-6 lg:px-8">
          <Link
            to="/books"
            className="mb-6 inline-flex items-center gap-1.5 rounded-lg text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
          >
            <Gi name="arrow-left" className="size-4" />
            Shelf
          </Link>

          <div className="flex flex-col gap-6 sm:flex-row sm:items-end">
            <div className="w-32 shrink-0 sm:w-40">
              <BookCover book={book} className="shadow-2xl ring-1 ring-white/10" />
            </div>

            <div className="min-w-0 flex-1">
              <h1 className="text-3xl font-semibold tracking-tight text-white sm:text-4xl">
                {book.title}
              </h1>

              <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-ink-300">
                {byline(book) && (
                  <span className="inline-flex items-center gap-1.5">
                    <Gi name="pencil" className="size-4 text-ink-500" />
                    {byline(book)}
                  </span>
                )}
                {publishYear(book) && (
                  <span className="inline-flex items-center gap-1.5">
                    <Gi name="calendar" className="size-4 text-ink-500" />
                    First published {publishYear(book)}
                  </span>
                )}
                {editions.length > 0 && (
                  <span className="inline-flex items-center gap-1.5">
                    <Gi name="book-pile" className="size-4 text-ink-500" />
                    {editions.length} printing{editions.length === 1 ? "" : "s"} on file
                  </span>
                )}
              </div>

              {book.subjects.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {book.subjects.slice(0, 6).map((subject) => (
                    <span
                      key={subject}
                      className="rounded-full bg-white/[0.07] px-2.5 py-1 text-xs text-ink-300"
                    >
                      {subject}
                    </span>
                  ))}
                </div>
              )}

              <div className="mt-5 flex max-w-md flex-wrap items-center gap-3">
                <StatusMenu entry={entry} size="md" statuses={STATUSES} />
                <ListenButton entry={entry} />
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="mx-auto grid max-w-5xl grid-cols-1 gap-5 px-4 pb-16 sm:px-6 lg:grid-cols-3 lg:px-8">
        <div className="space-y-5 lg:col-span-2">
          {book.description && (
            <Panel className="p-5">
              <h2 className="mb-2.5 text-sm font-semibold text-ink-200">About</h2>
              <p className="whitespace-pre-line text-sm leading-relaxed text-ink-400">
                {book.description}
              </p>
            </Panel>
          )}

          <BookFacts book={book} />

          <Editions editions={editions} />

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
              placeholder="Where you left off, what it reminded you of, who you'd lend it to…"
              className="w-full resize-y rounded-xl border border-white/[0.07] bg-ink-850 p-3 text-sm text-ink-100 placeholder:text-ink-600 focus:border-brand-500/50 focus-visible:focus-ring"
            />
          </Panel>
        </div>

        <div className="space-y-5">
          <Panel className="p-5">
            <h2 className="mb-3 text-sm font-semibold text-ink-200">Your rating</h2>
            <RatingPicker
              value={entry.user_rating}
              onChange={(rating) => update.mutate({ id: entry.id, patch: { user_rating: rating } })}
            />
          </Panel>

          <Panel className="p-5">
            <h2 className="mb-3 text-sm font-semibold text-ink-200">Timeline</h2>
            <dl className="space-y-2.5 text-sm">
              <Row
                label="Added"
                value={formatDate(entry.created_at)}
                hint={relativeTime(entry.created_at)}
              />
              <Row
                label="Started"
                value={formatDate(entry.started_at)}
                hint={relativeTime(entry.started_at)}
              />
              <Row
                label="Finished"
                value={formatDate(entry.finished_at)}
                hint={relativeTime(entry.finished_at)}
              />
            </dl>
          </Panel>

          <UnattachedFiles entryId={entry.id} />

          <ListMembership entry={entry} />

          <ProjectMembership entry={entry} />

          <Button
            variant="ghost"
            className="w-full text-red-400 hover:bg-red-500/10 hover:text-red-300"
            onClick={() => setConfirmDelete(true)}
          >
            <Gi name="trash" className="size-4" />
            Remove from shelf
          </Button>
        </div>
      </div>

      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)} label="Confirm removal">
        <h2 className="text-lg font-semibold text-ink-100">Remove {book.title}?</h2>
        <p className="mt-2 text-sm text-ink-400">
          This takes it off your shelf, along with your rating and notes. The book itself stays
          searchable, so you can add it again later.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
            Cancel
          </Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() => remove.mutate(entry.id, { onSuccess: () => navigate("/books") })}
          >
            Remove
          </Button>
        </div>
      </Dialog>
    </div>
  );
}

/**
 * The at-a-glance dossier: what Open Library knows that isn't the blurb. The
 * publisher and page count are read off the earliest printing that has them,
 * because the *work* has neither — only a printing does.
 */
function BookFacts({ book }: { book: Book }) {
  const editions = book.editions ?? [];
  const publishers = unique(editions.map((edition) => edition.publisher));
  const languages = unique(editions.map((edition) => edition.language));
  const pages = editions.find((edition) => edition.page_count)?.page_count ?? null;

  const rows: { label: string; items: string[] }[] = [
    { label: "Authors", items: book.authors ?? [] },
    { label: "Publishers", items: publishers.slice(0, 6) },
    { label: "Languages", items: languages.slice(0, 6) },
    { label: "Length", items: formatPages(pages) ? [formatPages(pages)] : [] },
    { label: "Subjects", items: (book.subjects ?? []).slice(0, 12) },
  ].filter((row) => row.items.length > 0);

  if (rows.length === 0) return null;

  return (
    <Panel className="p-5">
      <h2 className="mb-3 text-sm font-semibold text-ink-200">Details</h2>
      <dl className="space-y-3">
        {rows.map((row) => (
          <PillRow key={row.label} label={row.label} items={row.items} />
        ))}
      </dl>
    </Panel>
  );
}

/**
 * The listening entry point. Backhog has to be the player for the handoff to
 * work, so this opens the global player rather than handing the files to
 * anything else.
 *
 * Both queries use the same keys the player hydrates from, so the click that
 * starts playback is also a cache hit — the element gets its src inside the
 * tap's own task, which is what keeps iOS from refusing the play().
 */
function ListenButton({ entry }: { entry: BookEntry }) {
  const player = useAudioPlayer();
  const { data: timeline } = useQuery({
    queryKey: ["bookAudio", entry.id],
    queryFn: () => api.bookAudio(entry.id),
  });
  const { data: position } = useQuery({
    queryKey: ["bookPosition", entry.id],
    queryFn: () => api.bookPosition(entry.id),
  });

  if (!timeline || timeline.tracks.length === 0) return null;

  const active = player.entry?.id === entry.id;
  const into = position?.audio?.seconds ?? 0;
  // A finished book's stored position is the end of the tape; "resume" there
  // would be silence, so it starts over instead.
  const finished = timeline.total_duration > 0 && into >= timeline.total_duration - 30;

  return (
    <div className="flex items-center gap-2.5">
      <Button
        variant={active ? "secondary" : "primary"}
        onClick={() =>
          active ? player.toggle() : player.open(entry, finished ? { startAt: 0 } : undefined)
        }
      >
        <Gi name="headphones" className="size-3.5" />
        {active
          ? player.playing
            ? "Pause"
            : "Resume"
          : finished
            ? "Listen again"
            : into > 0
              ? "Resume"
              : "Listen"}
      </Button>
      <span className="text-xs text-ink-500">
        {formatDuration(timeline.total_duration)} · {timeline.tracks.length} file
        {timeline.tracks.length === 1 ? "" : "s"}
      </span>
    </div>
  );
}

/**
 * Every printing the provider knows about.
 *
 * Read-only, and deliberately so: the API takes `edition_id` when a book is
 * added, but the entry payload never returns it and PATCH /api/library/{id}
 * rejects the field, so there is nothing here to select against or save. When
 * the entry starts carrying its edition, this list becomes the picker.
 */
function Editions({ editions }: { editions: BookEdition[] }) {
  if (editions.length === 0) return null;

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Printings</h2>
      <p className="mb-3 text-xs text-ink-500">
        {editions.length} edition{editions.length === 1 ? "" : "s"} on file. You chose which one you
        own when you added the book.
      </p>
      <ul className="max-h-80 space-y-0.5 overflow-y-auto pr-1">
        {editions.map((edition) => (
          <li
            key={edition.id}
            className="flex items-baseline justify-between gap-3 rounded-lg px-2 py-1.5 text-sm"
          >
            <span className="min-w-0 flex-1 truncate text-ink-300">
              {editionLabel(edition) || "Unlabelled printing"}
            </span>
            {editionISBN(edition) && (
              <span className="shrink-0 text-[11px] tabular-nums text-ink-600">
                {editionISBN(edition)}
              </span>
            )}
          </li>
        ))}
      </ul>
    </Panel>
  );
}

/**
 * The bridge to the attach queue: when the NAS scan has files that look like
 * this book but nothing is attached yet, say so and link straight to the
 * review page rather than leaving the match sitting unnoticed.
 */
function UnattachedFiles({ entryId }: { entryId: string }) {
  const { data } = useQuery({ queryKey: ["media", "candidates"], queryFn: api.mediaCandidates });

  const waiting = (data?.candidates ?? []).filter((candidate) =>
    candidate.suggestions.some((suggestion) => suggestion.entry_id === entryId),
  );
  if (waiting.length === 0) return null;

  const files = waiting.reduce((count, candidate) => count + candidate.files.length, 0);

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Files on the NAS</h2>
      <p className="mb-3 text-xs leading-relaxed text-ink-500">
        {files} scanned file{files === 1 ? "" : "s"} look{files === 1 ? "s" : ""} like this book but
        {files === 1 ? " isn't" : " aren't"} attached yet.
      </p>
      <Link
        to="/books/files"
        className="inline-flex items-center gap-1.5 rounded-lg text-sm text-brand-400 transition-colors hover:text-brand-300 focus-visible:focus-ring"
      >
        <Gi name="full-folder" className="size-4" />
        Review in Book files
      </Link>
    </Panel>
  );
}

function PillRow({ label, items }: { label: string; items: string[] }) {
  return (
    <div className="flex flex-col gap-1.5 sm:flex-row sm:items-baseline sm:gap-3">
      <dt className="shrink-0 text-xs font-medium text-ink-500 sm:w-28">{label}</dt>
      <dd className="flex flex-wrap gap-1.5">
        {items.map((item) => (
          <span key={item} className="rounded-full bg-white/[0.07] px-2.5 py-1 text-xs text-ink-300">
            {item}
          </span>
        ))}
      </dd>
    </div>
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
            <Gi name="star" className="size-3 text-amber-300" />
            You rated this {value}/10 — click again to clear
          </>
        ) : (
          "Not rated yet"
        )}
      </p>
    </div>
  );
}

function unique(values: string[]): string[] {
  return [...new Set(values.filter(Boolean))];
}

function DetailSkeleton() {
  return (
    <div className="mx-auto max-w-5xl px-4 py-8 sm:px-6 lg:px-8">
      <div className="flex flex-col gap-6 sm:flex-row">
        <Skeleton className="h-56 w-32 shrink-0 sm:w-40" />
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
