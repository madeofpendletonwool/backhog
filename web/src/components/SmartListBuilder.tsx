import { useState } from "react";

import { Button, Input, Select, Gi } from "./ui/primitives";
import { useArena } from "@/hooks/useArena";
import { useSmartFields } from "@/hooks/useLists";
import { ruleSetTarget } from "@/lib/smartlists";
import {
  statusLabel,
  type MediaType,
  type Rule,
  type RuleSet,
  type SmartField,
  type Status,
} from "@/lib/types";

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

/**
 * The sort whitelist mirrors the server's smartSorts map. The media tag says
 * which arena a key orders meaningfully; the builder hides the other arena's
 * keys from a scoped set, exactly as it does rule fields.
 */
const SORT_FIELDS: { value: string; label: string; media?: string }[] = [
  { value: "added", label: "date added" },
  { value: "updated", label: "last updated" },
  { value: "name", label: "title" },
  { value: "user_rating", label: "my rating" },
  { value: "igdb_rating", label: "IGDB rating", media: "game" },
  { value: "hours_to_beat", label: "hours to beat", media: "game" },
  { value: "release_year", label: "release date", media: "game" },
  { value: "author", label: "author", media: "book" },
  { value: "published", label: "date published", media: "book" },
  { value: "pages", label: "page count", media: "book" },
];

const emptyRule = (field: SmartField): Rule => ({
  field: field.key,
  op: field.ops[0],
  value: field.type === "enum" ? (field.enum?.[0] ?? "") : field.type === "ref" ? [] : "",
});

/**
 * Builds a smart list rule set. Fields and operators come from the API so the
 * builder can never offer something the server-side whitelist would reject —
 * including the arena split: a set scoped to one arena (through its media_type
 * rules, or simply by being built from that arena) is offered that arena's
 * fields and sorts, never the other's.
 */
export function SmartListBuilder({
  value,
  onChange,
}: {
  value: RuleSet;
  onChange: (rules: RuleSet) => void;
}) {
  const { arena } = useArena();
  const { data } = useSmartFields();

  // A set scoped by its own rules decides its arena; an unscoped set is built
  // for the arena the user is standing in.
  const arenaMedia: MediaType = arena === "books" ? "book" : "game";
  const target = ruleSetTarget(value) ?? arenaMedia;
  // Saved rules stay visible and editable even when their field is out of
  // scope for this set — dropping one silently would change what the list
  // matches the moment the dialog opens. The dropdowns only offer in-scope
  // fields; the odd one out has to be removed by hand before saving.
  const allFields = data?.fields ?? [];
  const byKey = new Map(allFields.map((field) => [field.key, field]));
  const fields = allFields.filter((field) => !field.media || field.media === target);
  const sorts = SORT_FIELDS.filter((sort) => !sort.media || sort.media === target);
  const noun = target === "book" ? "book" : "game";

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

  // A saved sort key survives a scope change that hides it from the list.
  const sortOptions =
    value.sort && !sorts.some((s) => s.value === value.sort?.field)
      ? [...sorts, { value: value.sort.field, label: value.sort.field }]
      : sorts;

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
        <p className="rounded-xl border border-dashed border-edge-strong px-4 py-6 text-center text-xs text-ink-500">
          No conditions yet — this list would match every {noun} in your library.
        </p>
      )}

      <div className="space-y-2">
        {value.rules.map((rule, index) => {
          const field = byKey.get(rule.field);
          if (!field) return null;

          return (
            <div
              key={index}
              className="flex flex-wrap items-center gap-2 rounded-xl border border-edge bg-ink-850/60 p-2"
            >
              <Select
                value={rule.field}
                onChange={(event) => {
                  const next = byKey.get(event.target.value);
                  if (next) patchRule(index, emptyRule(next));
                }}
                className="h-9 w-auto min-w-[9rem] flex-1 text-xs"
              >
                {/* The rule's own field stays selectable even when out of
                    scope, so the row never silently rewrites itself. */}
                {!fields.some((f) => f.key === rule.field) && field && (
                  <option value={field.key}>{field.label}</option>
                )}
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
                <ValueInput
                  field={field}
                  rule={rule}
                  media={target}
                  onChange={(v) => patchRule(index, { value: v })}
                />
              )}

              <button
                onClick={() => removeRule(index)}
                aria-label="Remove condition"
                className="rounded-lg p-1.5 text-ink-600 transition-colors hover:bg-fill-hover hover:text-red-400 focus-visible:focus-ring"
              >
                <Gi name="x" className="size-4" />
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
          <Gi name="plus" className="size-3.5" />
          Add
        </Button>
      </div>

      <div className="flex items-center gap-2 border-t border-edge pt-3 text-xs text-ink-400">
        <Gi name="sparkles" className="size-3.5 shrink-0 text-hl-bright" />
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
          {sortOptions.map((option) => (
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

function ValueInput({
  field,
  rule,
  media,
  onChange,
}: {
  field: SmartField;
  rule: Rule;
  /** The arena whose words the enum labels use ("Reading", not "Playing"). */
  media: MediaType;
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
                {statusLabel(option as Status, media)}
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
    const labels =
      field.key === "status"
        ? (field.enum ?? []).map((option) => ({
            value: option,
            label: statusLabel(option as Status, media),
          }))
        : (field.enum ?? []).map((option) => ({ value: option, label: option }));

    return (
      <Select
        value={String(rule.value ?? "")}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 w-auto min-w-[8rem] flex-1 text-xs"
      >
        {labels.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
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
