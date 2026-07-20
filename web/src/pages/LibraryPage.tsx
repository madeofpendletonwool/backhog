import { cn } from "@/lib/cn";
import { LayoutGrid, Rows3, Search, SlidersHorizontal, X } from "lucide-react";
import { useState } from "react";
import { useOutletContext } from "react-router-dom";

import { GameCard, GameCardSkeleton } from "@/components/GameCard";
import { GameTable } from "@/components/GameTable";
import { StatsStrip } from "@/components/StatsStrip";
import { Button, EmptyState, Input, Select } from "@/components/ui/primitives";
import { useDebounced, useFacets, useLibrary } from "@/hooks/useLibrary";
import { STATUS_LABELS, STATUSES } from "@/lib/types";

const SORTS = [
  { value: "added", label: "Recently added" },
  { value: "name", label: "Title A–Z" },
  { value: "released", label: "Newest release" },
  { value: "rating", label: "Highest rated" },
  { value: "shortest", label: "Shortest first" },
  { value: "longest", label: "Longest first" },
  { value: "updated", label: "Recently updated" },
];

export function LibraryPage() {
  const { openAddDialog } = useOutletContext<{ openAddDialog: () => void }>();

  const [status, setStatus] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState("added");
  const [platform, setPlatform] = useState<string>("");
  const [genre, setGenre] = useState<string>("");
  const [view, setView] = useState<"grid" | "table">("grid");
  const [filtersOpen, setFiltersOpen] = useState(false);

  const debouncedSearch = useDebounced(search, 250);
  const { data: facets } = useFacets();

  const {
    data,
    isLoading,
    isPlaceholderData,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useLibrary({
    status,
    q: debouncedSearch,
    sort,
    platform: platform ? Number(platform) : undefined,
    genre: genre ? Number(genre) : undefined,
  });

  const entries = data?.pages.flatMap((page) => page.entries) ?? [];
  const total = data?.pages[0]?.total ?? 0;
  const hasFilters = Boolean(status || debouncedSearch || platform || genre);

  const clearFilters = () => {
    setStatus("");
    setSearch("");
    setPlatform("");
    setGenre("");
  };

  return (
    <div className="mx-auto max-w-[1600px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Library</h1>
        <p className="mt-1 text-sm text-ink-400">
          {data ? `${total} game${total === 1 ? "" : "s"}` : "Loading…"}
          {hasFilters && " matching your filters"}
          {entries.length < total && ` · showing ${entries.length}`}
        </p>
      </header>

      <div className="mb-6">
        <StatsStrip />
      </div>

      {/* Status tabs — the primary axis people slice by. */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <StatusTab active={status === ""} onClick={() => setStatus("")}>
          All
        </StatusTab>
        {STATUSES.map((value) => (
          <StatusTab key={value} active={status === value} onClick={() => setStatus(value)}>
            {STATUS_LABELS[value]}
          </StatusTab>
        ))}

        <div className="ml-auto flex items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-ink-500" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Filter by title…"
              aria-label="Filter library by title"
              className="w-44 pl-9 sm:w-56"
            />
          </div>

          <Button
            size="icon"
            variant={filtersOpen || platform || genre ? "primary" : "secondary"}
            onClick={() => setFiltersOpen((open) => !open)}
            aria-label="More filters"
            aria-expanded={filtersOpen}
          >
            <SlidersHorizontal className="size-4" />
          </Button>

          <div className="flex rounded-xl border border-white/[0.07] bg-ink-850 p-0.5">
            <ViewToggle active={view === "grid"} onClick={() => setView("grid")} label="Grid view">
              <LayoutGrid className="size-4" />
            </ViewToggle>
            <ViewToggle active={view === "table"} onClick={() => setView("table")} label="Table view">
              <Rows3 className="size-4" />
            </ViewToggle>
          </div>
        </div>
      </div>

      {filtersOpen && (
        <div className="animate-fade-rise panel mb-5 flex flex-wrap items-end gap-3 p-4">
          <FilterSelect
            label="Sort by"
            value={sort}
            onChange={setSort}
            options={SORTS.map((s) => ({ value: s.value, label: s.label }))}
          />
          <FilterSelect
            label="Platform"
            value={platform}
            onChange={setPlatform}
            placeholder="Any platform"
            options={(facets?.platforms ?? []).map((p) => ({ value: String(p.id), label: p.name }))}
          />
          <FilterSelect
            label="Genre"
            value={genre}
            onChange={setGenre}
            placeholder="Any genre"
            options={(facets?.genres ?? []).map((g) => ({ value: String(g.id), label: g.name }))}
          />
          {hasFilters && (
            <Button variant="ghost" onClick={clearFilters}>
              <X className="size-4" />
              Clear
            </Button>
          )}
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {Array.from({ length: 14 }).map((_, index) => (
            <GameCardSkeleton key={index} />
          ))}
        </div>
      ) : entries.length === 0 ? (
        hasFilters ? (
          <EmptyState
            icon={<Search className="size-7" />}
            title="No games match"
            description="Try loosening the filters, or search for something new to add."
            action={
              <Button variant="secondary" onClick={clearFilters}>
                Clear filters
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={<span className="text-3xl">🐗</span>}
            title="Your backlog is empty"
            description="Suspiciously empty. Add the games you own but haven't gotten around to — that's what this is for."
            action={
              <Button variant="primary" size="lg" onClick={openAddDialog}>
                Add your first game
              </Button>
            }
          />
        )
      ) : (
        <div className={cn("transition-opacity", isPlaceholderData && "opacity-60")}>
          {view === "grid" ? (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
              {entries.map((entry) => (
                <GameCard key={entry.id} entry={entry} />
              ))}
            </div>
          ) : (
            <GameTable entries={entries} />
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
          ? "bg-white/[0.09] text-ink-100"
          : "text-ink-400 hover:bg-white/[0.05] hover:text-ink-200",
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
        active ? "bg-white/[0.09] text-ink-100" : "text-ink-500 hover:text-ink-300",
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
