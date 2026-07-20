import { cn } from "@/lib/cn";
import { Hand, Sparkles } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { SmartListBuilder } from "./SmartListBuilder";
import { Button, Input } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { useCreateList } from "@/hooks/useLists";
import type { RuleSet } from "@/lib/types";

const DEFAULT_RULES: RuleSet = {
  match: "all",
  rules: [{ field: "status", op: "eq", value: "backlog" }],
  sort: { field: "added", dir: "desc" },
};

export function CreateListDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const create = useCreateList();

  const [kind, setKind] = useState<"manual" | "smart">("manual");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [rules, setRules] = useState<RuleSet>(DEFAULT_RULES);

  const reset = () => {
    setKind("manual");
    setName("");
    setDescription("");
    setRules(DEFAULT_RULES);
    create.reset();
  };

  const close = () => {
    reset();
    onClose();
  };

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    create.mutate(
      {
        name: name.trim(),
        description: description.trim(),
        kind,
        rules: kind === "smart" ? rules : undefined,
      },
      {
        onSuccess: (list) => {
          close();
          navigate(`/lists/${list.id}`);
        },
      },
    );
  };

  return (
    <Dialog open={open} onClose={close} label="Create a list" className="max-w-xl">
      <h2 className="text-lg font-semibold text-ink-100">New list</h2>

      <form onSubmit={submit} className="mt-5 space-y-4">
        <div className="grid grid-cols-2 gap-2">
          <KindOption
            active={kind === "manual"}
            onClick={() => setKind("manual")}
            icon={<Hand className="size-4" />}
            title="Manual"
            description="You choose what goes in."
          />
          <KindOption
            active={kind === "smart"}
            onClick={() => setKind("smart")}
            icon={<Sparkles className="size-4" />}
            title="Smart"
            description="Rules decide, always current."
          />
        </div>

        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-300">Name</span>
          <Input
            required
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={kind === "smart" ? "Short and sweet" : "Summer 2026"}
          />
        </label>

        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-300">
            Description <span className="text-ink-600">(optional)</span>
          </span>
          <Input
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="What belongs in here?"
          />
        </label>

        {kind === "smart" && (
          <div className="rounded-xl border border-white/[0.06] bg-ink-900/50 p-3">
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
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!name.trim()}>
            Create list
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
          : "border-white/[0.07] bg-ink-850 hover:border-white/[0.14]",
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
