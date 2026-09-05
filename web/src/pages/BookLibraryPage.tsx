import { cn } from "@/lib/cn";
import { useEffect, useState } from "react";
import { useOutletContext, useSearchParams } from "react-router-dom";

import { BookCard, BookCardSkeleton } from "@/components/BookCard";
import { BookStatsStrip } from "@/components/BookStatsStrip";
import { BookTable } from "@/components/BookTable";
import { Gi } from "@/components/ui/Gi";
import { Button, EmptyState, Input, Select } from "@/components/ui/primitives";
import { useBookFacets, useBookLibrary } from "@/hooks/useBooks";
import { useDebounced } from "@/hooks/useLibrary";
import { usePersistentState } from "@/hooks/usePersistentState";
import { BOOK_STATUS_LABELS, QUICK_STATUSES, isBookEntry } from "@/lib/types";

const SORTS = [
  { value: "added", label: "Recently added" },
  { value: "title", label: "Title A–Z" },
  { value: "author", label: "Author A–Z" },
  { value: "published", label: "Newest first" },
  { value: "pages", label: "Shortest first" },
  { value: "updated", label: "Recently updated" },
];

/**
 * The shelf. Deliberately the same page as LibraryPage — same status tabs,
 * same grid/table toggle, same persisted filters — because the two arenas are
 * one app and a reader should not have to learn a second set of controls. The
 * axes are the ones a shelf actually has: author, subject, language.
 */
export function BookLibraryPage() {
  const { openAddDialog } = useOutletContext<{ openAddDialog: () => void }>();

  const [status, setStatus] = usePersistentState("backhog:books:status", "");
  const [sort, setSort] = usePersistentState("backhog:books:sort", "title");
  const [author, setAuthor] = usePersistentState<string>("backhog:books:author", "");
  const [subject, setSubject] = usePersistentState<string>("backhog:books:subject", "");
  const [language, setLanguage] = usePersistentState<string>("backhog:books:language", "");
  const [view, setView] = usePersistentState<"grid" | "table">("backhog:books:view", "grid");
  const [search, setSearch] = useState("");
  const [filtersOpen, setFiltersOpen] = useState(false);

  /* Deep links into the shelf — the author and subject links in the
     full-screen player, above all. The filters themselves stay where they
     are, in localStorage: they are a personal preference that should survive
     leaving the page, not a location. So an incoming ?author= is *adopted*
     into that state and then stripped from the URL, which does mean the
     address bar reads a bare /books once you have landed. Making every filter
     URL-driven instead would mean rewriting the same model in LibraryPage to
     keep the two arenas' shelves identical, which is the whole reason they
     share these controls. */
  const [searchParams, setSearchParams] = useSearchParams();
  useEffect(() => {
    if (![...searchParams.keys()].length) return;
    // The whole filter set is replaced, not merged: "every book by this
    // author" has to mean that, and a subject left over from last week's
    // browsing would quietly hand back an empty shelf instead.
    setAuthor(searchParams.get("author") ?? "");
    setSubject(searchParams.get("subject") ?? "");
    setLanguage(searchParams.get("language") ?? "");
    setStatus(searchParams.get("status") ?? "");
    setSearch(searchParams.get("q") ?? "");
    setSearchParams({}, { replace: true });
  }, [searchParams, setAuthor, setLanguage, setSearchParams, setStatus, setSubject]);

  const debouncedSearch = useDebounced(search, 250);
  const { data: facets } = useBookFacets();

  const {
    data,
    isLoading,
    isPlaceholderData,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useBookLibrary({
    status,
    q: debouncedSearch,
    sort,
    author: author || undefined,
    subject: subject || undefined,
    language: language || undefined,
  });

  // media=book is pinned on the query, so the guard is a type narrowing rather
  // than a filter that does any work.
  const entries = (data?.pages.flatMap((page) => page.entries) ?? []).filter(isBookEntry);
  const total = data?.pages[0]?.total ?? 0;
  const hasFilters = Boolean(status || debouncedSearch || author || subject || language);

  const clearFilters = () => {
    setStatus("");
    setSearch("");
    setAuthor("");
    setSubject("");
    setLanguage("");
  };

  return (
    <div className="mx-auto max-w-[1600px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Shelf</h1>
        <p className="mt-1 text-sm text-ink-400">
          {data ? `${total} book${total === 1 ? "" : "s"}` : "Loading…"}
          {hasFilters && " matching your filters"}
          {entries.length < total && ` · showing ${entries.length}`}
        </p>
      </header>

      <div className="mb-6">
        <BookStatsStrip />
      </div>

      {/* Status tabs — the shelf you are looking at. */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <StatusTab active={status === ""} onClick={() => setStatus("")}>
          All
        </StatusTab>
        {QUICK_STATUSES.map((value) => (
          <StatusTab key={value} active={status === value} onClick={() => setStatus(value)}>
            {BOOK_STATUS_LABELS[value]}
          </StatusTab>
        ))}

        <div className="ml-auto flex items-center gap-2">
          <div className="relative">
            <Gi
              name="search"
              className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-ink-500"
            />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Filter by title…"
              aria-label="Filter shelf by title"
              className="w-44 pl-9 sm:w-56"
            />
          </div>

          <Button
            size="icon"
            variant={filtersOpen || author || subject || language ? "primary" : "secondary"}
            onClick={() => setFiltersOpen((open) => !open)}
            aria-label="More filters"
            aria-expanded={filtersOpen}
          >
            <Gi name="sliders" className="size-4" />
          </Button>

          <div className="flex rounded-xl border border-edge bg-ink-850 p-0.5">
            <ViewToggle active={view === "grid"} onClick={() => setView("grid")} label="Grid view">
              <Gi name="layout-grid" className="size-4" />
            </ViewToggle>
            <ViewToggle active={view === "table"} onClick={() => setView("table")} label="Table view">
              <Gi name="rows" className="size-4" />
            </ViewToggle>
          </div>
        </div>
      </div>

      {filtersOpen && (
        <div className="animate-fade-rise panel mb-5 flex flex-wrap items-end gap-3 p-4">
          <FilterSelect label="Sort by" value={sort} onChange={setSort} options={SORTS} />
          <FilterSelect
            label="Author"
            value={author}
            onChange={setAuthor}
            placeholder="Any author"
            options={(facets?.authors ?? []).map((name) => ({ value: name, label: name }))}
          />
          <FilterSelect
            label="Subject"
            value={subject}
            onChange={setSubject}
            placeholder="Any subject"
            options={(facets?.subjects ?? []).map((name) => ({ value: name, label: name }))}
          />
          <FilterSelect
            label="Language"
            value={language}
            onChange={setLanguage}
            placeholder="Any language"
            options={(facets?.languages ?? []).map((code) => ({ value: code, label: code }))}
          />
          {hasFilters && (
            <Button variant="ghost" onClick={clearFilters}>
              <Gi name="x" className="size-4" />
              Clear
            </Button>
          )}
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {Array.from({ length: 14 }).map((_, index) => (
            <BookCardSkeleton key={index} />
          ))}
        </div>
      ) : entries.length === 0 ? (
        hasFilters ? (
          <EmptyState
            icon={<Gi name="search" className="size-7" />}
            title="No books match"
            description="Try loosening the filters, or add something new to the shelf."
            action={
              <Button variant="secondary" onClick={clearFilters}>
                Clear filters
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={<Gi name="book-pile" className="size-7 text-ink-500" />}
            title="Nothing on the shelf yet"
            description="Point the camera at the barcode on the back of a book you own — or type the ISBN, or search for it by title."
            action={
              <Button variant="primary" size="lg" onClick={openAddDialog}>
                Add your first book
              </Button>
            }
          />
        )
      ) : (
        <div className={cn("transition-opacity", isPlaceholderData && "opacity-60")}>
          {view === "grid" ? (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
              {entries.map((entry) => (
                <BookCard key={entry.id} entry={entry} />
              ))}
            </div>
          ) : (
            <BookTable entries={entries} />
          )}

          {hasNextPage && (
            <div className="mt-8 flex flex-col items-center gap-2">
              <Button
                variant="secondary"
                size="lg"
                loading={isFetchingNextPage}
                onClick={() => fetchNextPage()}
              >
                Load more
              </Button>
              <p className="text-xs text-ink-500">
                {entries.length} of {total}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function StatusTab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "rounded-xl px-3.5 py-2 text-sm font-medium transition-colors focus-visible:focus-ring",
        active
          ? "bg-fill-active text-ink-100"
          : "text-ink-400 hover:bg-fill-hover hover:text-ink-200",
      )}
    >
      {children}
    </button>
  );
}

function ViewToggle({
  active,
  onClick,
  label,
  children,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      aria-pressed={active}
      className={cn(
        "rounded-[0.6rem] p-2 transition-colors focus-visible:focus-ring",
        active ? "bg-fill-active text-ink-100" : "text-ink-500 hover:text-ink-300",
      )}
    >
      {children}
    </button>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  placeholder?: string;
}) {
  return (
    <label className="block min-w-[10rem] flex-1">
      <span className="mb-1.5 block text-xs font-medium text-ink-400">{label}</span>
      <Select value={value} onChange={(event) => onChange(event.target.value)}>
        {placeholder && <option value="">{placeholder}</option>}
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </Select>
    </label>
  );
}
