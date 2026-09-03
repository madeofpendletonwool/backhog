import { cn } from "@/lib/cn";
import { Gi } from "./ui/Gi";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { SmartListBuilder } from "./SmartListBuilder";
import { Button, Input } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { useCreateProject } from "@/hooks/useProjects";
import type { ProjectKind, RuleSet } from "@/lib/types";

const DEFAULT_RULES: RuleSet = {
  match: "all",
  rules: [{ field: "status", op: "eq", value: "backlog" }],
  sort: { field: "added", dir: "desc" },
};

export function CreateProjectDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const create = useCreateProject();

  const [kind, setKind] = useState<ProjectKind>("checklist");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [target, setTarget] = useState("");
  const [rules, setRules] = useState<RuleSet>(DEFAULT_RULES);

  const reset = () => {
    setKind("checklist");
    setName("");
    setDescription("");
    setTarget("");
    setRules(DEFAULT_RULES);
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
            description="Finish these games."
          />
          <KindOption
            active={kind === "count_goal"}
            onClick={() => setKind("count_goal")}
            icon={<Gi name="target" className="size-4" />}
            title="Count goal"
            description="Finish N games."
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
                ? "Finish the Souls games"
                : kind === "count_goal"
                  ? "10 games before buying another"
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
                <span className="text-ink-600"> (empty = every matching game)</span>
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
