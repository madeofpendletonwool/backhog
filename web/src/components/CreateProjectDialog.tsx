import { cn } from "@/lib/cn";
import { Gi } from "./ui/Gi";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { SmartListBuilder } from "./SmartListBuilder";
import { Button, Input } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { useArena } from "@/hooks/useArena";
import { useCreateProject } from "@/hooks/useProjects";
import type { MediaType, ProjectKind, RuleSet } from "@/lib/types";

/** A rule goal starts scoped to the arena it was built in. */
const defaultRules = (media: MediaType): RuleSet => ({
  match: "all",
  rules: [
    { field: "media_type", op: "eq", value: media },
    { field: "status", op: "eq", value: "backlog" },
  ],
  sort: { field: "added", dir: "desc" },
});

export function CreateProjectDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const create = useCreateProject();
  const { arena } = useArena();
  const media: MediaType = arena === "books" ? "book" : "game";
  const books = arena === "books";
  const noun = books ? "book" : "game";
  const nouns = books ? "books" : "games";

  const [kind, setKind] = useState<ProjectKind>("checklist");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [target, setTarget] = useState("");
  const [rules, setRules] = useState<RuleSet>(defaultRules(media));

  const reset = () => {
    setKind("checklist");
    setName("");
    setDescription("");
    setTarget("");
    setRules(defaultRules(media));
    create.reset();
  };

  const close = () => {
    reset();
    onClose();
  };

  const targetNumber = Number(target);

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    create.mutate(
      {
        name: name.trim(),
        description: description.trim(),
        kind,
        media,
        target_count:
          kind === "count_goal"
            ? targetNumber
            : kind === "rule_goal" && target.trim() !== ""
              ? targetNumber
              : null,
        rules: kind === "rule_goal" ? rules : undefined,
      },
      {
        onSuccess: (project) => {
          close();
          navigate(`/projects/${project.id}`);
        },
      },
    );
  };

  const valid =
    name.trim().length > 0 &&
    (kind !== "count_goal" || (target.trim() !== "" && targetNumber > 0)) &&
    (kind !== "rule_goal" || target.trim() === "" || targetNumber > 0);

  return (
    <Dialog open={open} onClose={close} label="Create a project" className="max-w-xl">
      <h2 className="text-lg font-semibold text-ink-100">New project</h2>
      <p className="mt-1 text-xs text-ink-500">
        Lists are what exists. Projects are what you're trying to accomplish.
      </p>

      <form onSubmit={submit} className="mt-5 space-y-4">
        <div className="grid grid-cols-3 gap-2">
          <KindOption
            active={kind === "checklist"}
            onClick={() => setKind("checklist")}
            icon={<Gi name="list-checks" className="size-4" />}
            title="Checklist"
            description={`Finish these ${nouns}.`}
          />
          <KindOption
            active={kind === "count_goal"}
            onClick={() => setKind("count_goal")}
            icon={<Gi name="target" className="size-4" />}
            title="Count goal"
            description={`Finish N ${nouns}.`}
          />
          <KindOption
            active={kind === "rule_goal"}
            onClick={() => setKind("rule_goal")}
            icon={<Gi name="sparkles" className="size-4" />}
            title="Rule goal"
            description="Clear a rule-defined set."
          />
        </div>

        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-300">Name</span>
          <Input
            required
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={
              kind === "checklist"
                ? books
                  ? "Read the Dune books"
                  : "Finish the Souls games"
                : kind === "count_goal"
                  ? books
                    ? "10 books before buying another"
                    : "10 games before buying another"
                  : books
                    ? "Read my classics shelf"
                    : "Play my PS2 backlog"
            }
          />
        </label>

        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-300">
            Description <span className="text-ink-600">(optional)</span>
          </span>
          <Input
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="What does 'done' mean here?"
          />
        </label>

        {kind !== "checklist" && (
          <label className="block">
            <span className="mb-1.5 block text-xs font-medium text-ink-300">
              {kind === "count_goal" ? "Target" : "Target"}
              {kind === "rule_goal" && (
                <span className="text-ink-600"> (empty = every matching {noun})</span>
              )}
            </span>
            <Input
              type="number"
              min={1}
              step={1}
              required={kind === "count_goal"}
              value={target}
              onChange={(event) => setTarget(event.target.value)}
              placeholder={kind === "count_goal" ? "10" : "5"}
              className="w-28"
            />
          </label>
        )}

        {kind === "rule_goal" && (
          <div className="rounded-xl border border-edge bg-ink-900/50 p-3">
            <SmartListBuilder value={rules} onChange={setRules} />
          </div>
        )}

        {create.isError && (
          <p role="alert" className="rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
            {(create.error as Error).message}
          </p>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="ghost" onClick={close}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!valid}>
            Create project
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function KindOption({
  active,
  onClick,
  icon,
  title,
  description,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "rounded-xl border p-3 text-left transition-colors focus-visible:focus-ring",
        active
          ? "border-brand-500/50 bg-brand-500/10"
          : "border-edge bg-ink-850 hover:border-edge-strong",
      )}
    >
      <span className={cn("flex items-center gap-2 text-sm font-medium", active ? "text-brand-300" : "text-ink-200")}>
        {icon}
        {title}
      </span>
      <span className="mt-1 block text-xs text-ink-500">{description}</span>
    </button>
  );
}
