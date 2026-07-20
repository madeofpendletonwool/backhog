import { Plus, Sparkles, X } from "lucide-react";
import { useState } from "react";

import { Button, Input, Select } from "./ui/primitives";
import { useSmartFields } from "@/hooks/useLists";
import { STATUS_LABELS, type Rule, type RuleSet, type SmartField, type Status } from "@/lib/types";

const OP_LABELS: Record<string, string> = {
  eq: "is",
  neq: "is not",
  gt: "is more than",
  lt: "is less than",
  gte: "is at least",
  lte: "is at most",
  contains: "contains",
  in: "is any of",
  not_in: "is none of",
  is_null: "is not set",
  not_null: "is set",
};

const VALUELESS_OPS = new Set(["is_null", "not_null"]);

const emptyRule = (field: SmartField): Rule => ({
  field: field.key,
  op: field.ops[0],
  value: field.type === "enum" ? (field.enum?.[0] ?? "") : field.type === "ref" ? [] : "",
});

/**
 * Builds a smart list rule set. Fields and operators come from the API so the
 * builder can never offer something the server-side whitelist would reject.
 */
export function SmartListBuilder({
  value,
  onChange,
}: {
  value: RuleSet;
  onChange: (rules: RuleSet) => void;
}) {
  const { data } = useSmartFields();
  const fields = data?.fields ?? [];
  const byKey = new Map(fields.map((field) => [field.key, field]));

  const [newFieldKey, setNewFieldKey] = useState("");

  const patchRule = (index: number, patch: Partial<Rule>) => {
    const rules = value.rules.map((rule, i) => (i === index ? { ...rule, ...patch } : rule));
    onChange({ ...value, rules });
  };

  const removeRule = (index: number) => {
    onChange({ ...value, rules: value.rules.filter((_, i) => i !== index) });
  };

  const addRule = () => {
    const field = byKey.get(newFieldKey) ?? fields[0];
    if (!field) return;
    onChange({ ...value, rules: [...value.rules, emptyRule(field)] });
    setNewFieldKey("");
  };

  if (fields.length === 0) {
    return <p className="text-sm text-ink-500">Loading fields…</p>;
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-sm text-ink-400">
        <span>Match</span>
        <Select
          value={value.match}
          onChange={(event) => onChange({ ...value, match: event.target.value as "all" | "any" })}
          className="h-8 w-auto px-2 text-xs"
        >
          <option value="all">all</option>
          <option value="any">any</option>
        </Select>
        <span>of these conditions:</span>
      </div>

      {value.rules.length === 0 && (
        <p className="rounded-xl border border-dashed border-white/10 px-4 py-6 text-center text-xs text-ink-500">
          No conditions yet — this list would match every game in your library.
        </p>
      )}

      <div className="space-y-2">
        {value.rules.map((rule, index) => {
          const field = byKey.get(rule.field);
          if (!field) return null;

          return (
            <div
              key={index}
              className="flex flex-wrap items-center gap-2 rounded-xl border border-white/[0.06] bg-ink-850/60 p-2"
            >
              <Select
                value={rule.field}
                onChange={(event) => {
                  const next = byKey.get(event.target.value);
                  if (next) patchRule(index, emptyRule(next));
                }}
                className="h-9 w-auto min-w-[9rem] flex-1 text-xs"
              >
                {fields.map((option) => (
                  <option key={option.key} value={option.key}>
                    {option.label}
                  </option>
                ))}
              </Select>

              <Select
                value={rule.op}
                onChange={(event) => patchRule(index, { op: event.target.value })}
                className="h-9 w-auto min-w-[7rem] text-xs"
              >
                {field.ops.map((op) => (
                  <option key={op} value={op}>
                    {OP_LABELS[op] ?? op}
                  </option>
                ))}
              </Select>

              {!VALUELESS_OPS.has(rule.op) && (
                <ValueInput field={field} rule={rule} onChange={(v) => patchRule(index, { value: v })} />
              )}

              <button
                onClick={() => removeRule(index)}
                aria-label="Remove condition"
                className="rounded-lg p-1.5 text-ink-600 transition-colors hover:bg-white/[0.06] hover:text-red-400 focus-visible:focus-ring"
              >
                <X className="size-4" />
              </button>
            </div>
          );
        })}
      </div>

      <div className="flex items-center gap-2">
        <Select
          value={newFieldKey}
          onChange={(event) => setNewFieldKey(event.target.value)}
          className="h-9 w-auto flex-1 text-xs"
        >
          <option value="">Add a condition…</option>
          {fields.map((field) => (
            <option key={field.key} value={field.key}>
              {field.label}
            </option>
          ))}
        </Select>
        <Button size="sm" onClick={addRule} disabled={!newFieldKey}>
          <Plus className="size-3.5" />
          Add
        </Button>
      </div>

      <div className="flex items-center gap-2 border-t border-white/[0.06] pt-3 text-xs text-ink-400">
        <Sparkles className="size-3.5 shrink-0 text-brand-400" />
        <span>Sort by</span>
        <Select
          value={value.sort?.field ?? "added"}
          onChange={(event) =>
            onChange({
              ...value,
              sort: { field: event.target.value, dir: value.sort?.dir ?? "desc" },
            })
          }
          className="h-8 w-auto flex-1 px-2 text-xs"
        >
          {SORT_FIELDS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select
          value={value.sort?.dir ?? "desc"}
          onChange={(event) =>
            onChange({
              ...value,
              sort: { field: value.sort?.field ?? "added", dir: event.target.value as "asc" | "desc" },
            })
          }
          className="h-8 w-auto px-2 text-xs"
        >
          <option value="asc">ascending</option>
          <option value="desc">descending</option>
        </Select>
      </div>
    </div>
  );
}

// Mirrors the server's smartSorts whitelist.
const SORT_FIELDS = [
  { value: "added", label: "date added" },
  { value: "updated", label: "last updated" },
  { value: "name", label: "title" },
  { value: "igdb_rating", label: "IGDB rating" },
  { value: "user_rating", label: "my rating" },
  { value: "hours_to_beat", label: "hours to beat" },
  { value: "release_year", label: "release date" },
];

function ValueInput({
  field,
  rule,
  onChange,
}: {
  field: SmartField;
  rule: Rule;
  onChange: (value: Rule["value"]) => void;
}) {
  // Multi-value operators send an array; the server expects names for refs.
  if (rule.op === "in" || rule.op === "not_in") {
    const values = Array.isArray(rule.value) ? rule.value : [];

    if (field.type === "enum") {
      return (
        <div className="flex flex-wrap gap-1">
          {(field.enum ?? []).map((option) => {
            const active = values.includes(option);
            return (
              <button
                key={option}
                onClick={() =>
                  onChange(
                    active ? values.filter((v) => v !== option) : [...values, option],
                  )
                }
                className={
                  active
                    ? "rounded-lg bg-brand-600 px-2 py-1.5 text-xs font-medium text-white"
                    : "rounded-lg bg-ink-800 px-2 py-1.5 text-xs text-ink-400 hover:text-ink-200"
                }
              >
                {STATUS_LABELS[option as Status] ?? option}
              </button>
            );
          })}
        </div>
      );
    }

    return (
      <Input
        value={values.join(", ")}
        onChange={(event) =>
          onChange(
            event.target.value
              .split(",")
              .map((part) => part.trim())
              .filter(Boolean),
          )
        }
        placeholder="RPG, Indie"
        className="h-9 w-auto min-w-[9rem] flex-1 text-xs"
      />
    );
  }

  if (field.type === "enum") {
    return (
      <Select
        value={String(rule.value ?? "")}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 w-auto min-w-[8rem] flex-1 text-xs"
      >
        {(field.enum ?? []).map((option) => (
          <option key={option} value={option}>
            {STATUS_LABELS[option as Status] ?? option}
          </option>
        ))}
      </Select>
    );
  }

  if (field.type === "number") {
    return (
      <Input
        type="number"
        step="any"
        value={rule.value == null ? "" : String(rule.value)}
        // Numbers must go over the wire as numbers: the server type-checks them.
        onChange={(event) =>
          onChange(event.target.value === "" ? "" : Number(event.target.value))
        }
        className="h-9 w-24 text-xs"
      />
    );
  }

  return (
    <Input
      value={String(rule.value ?? "")}
      onChange={(event) => onChange(event.target.value)}
      className="h-9 w-auto min-w-[8rem] flex-1 text-xs"
    />
  );
}
