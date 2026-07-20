import { ListTree, Plus, Sparkles } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { CreateListDialog } from "@/components/CreateListDialog";
import { Button, EmptyState, Skeleton } from "@/components/ui/primitives";
import { useLists } from "@/hooks/useLists";

export function ListsPage() {
  const { data, isLoading } = useLists();
  const [creating, setCreating] = useState(false);

  const lists = data?.lists ?? [];
  const manual = lists.filter((list) => list.kind === "manual");
  const smart = lists.filter((list) => list.kind === "smart");

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Lists</h1>
          <p className="mt-1 text-sm text-ink-400">
            Hand-picked collections, and smart lists that keep themselves current.
          </p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          New list
        </Button>
      </header>

      {isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-24" />
          ))}
        </div>
      ) : lists.length === 0 ? (
        <EmptyState
          icon={<ListTree className="size-7" />}
          title="No lists yet"
          description="Group games however you like — a manual list you curate, or a smart list defined by rules."
          action={
            <Button variant="primary" onClick={() => setCreating(true)}>
              Create a list
            </Button>
          }
        />
      ) : (
        <div className="space-y-8">
          {smart.length > 0 && (
            <Section
              title="Smart lists"
              caption="Defined by rules, always up to date."
              lists={smart}
            />
          )}
          {manual.length > 0 && (
            <Section title="Your lists" caption="Curated by hand." lists={manual} />
          )}
        </div>
      )}

      <CreateListDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function Section({
  title,
  caption,
  lists,
}: {
  title: string;
  caption: string;
  lists: { id: string; name: string; description: string; kind: string; count: number }[];
}) {
  return (
    <section>
      <div className="mb-3">
        <h2 className="text-sm font-semibold text-ink-200">{title}</h2>
        <p className="text-xs text-ink-500">{caption}</p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {lists.map((list) => (
          <Link
            key={list.id}
            to={`/lists/${list.id}`}
            className="panel group p-4 transition-all duration-200 hover:-translate-y-0.5 hover:border-white/[0.14] focus-visible:focus-ring"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="flex items-center gap-1.5 truncate font-medium text-ink-100">
                  {list.kind === "smart" && (
                    <Sparkles className="size-3.5 shrink-0 text-brand-400" />
                  )}
                  {list.name}
                </p>
                {list.description && (
                  <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-ink-500">
                    {list.description}
                  </p>
                )}
              </div>
              <span className="shrink-0 rounded-lg bg-white/[0.06] px-2 py-1 text-xs font-medium tabular-nums text-ink-300">
                {list.count}
              </span>
            </div>
          </Link>
        ))}
      </div>
    </section>
  );
}
