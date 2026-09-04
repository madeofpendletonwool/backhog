import type { MediaType, RuleSet } from "./types";

/**
 * The client-side half of the media scoping decision the server documents in
 * internal/store/smartlists.go: a rule set carries its arena scope at the set
 * level, as media_type rules, rather than per field. This mirror answers the
 * one question the UI needs before the server is asked — "which arena does
 * this set target?" — so the builder can offer the right fields and the
 * sidebar can file a smart list under the right arena. null means unscoped:
 * the set admits both arenas and belongs to both.
 */
export function ruleSetTarget(rs: RuleSet | undefined | null): MediaType | null {
  if (!rs) return null;

  const admits = (rule: RuleSet["rules"][number], media: MediaType): boolean => {
    if (rule.field !== "media_type") return false;
    if (rule.op === "eq") return rule.value === media;
    if (rule.op === "in") {
      const values = Array.isArray(rule.value) ? rule.value.map(String) : [];
      return values.length > 0 && values.every((v) => v === media);
    }
    return false;
  };

  for (const rule of rs.rules) {
    if (admits(rule, "book")) return "book";
  }
  for (const rule of rs.rules) {
    if (admits(rule, "game")) return "game";
  }
  return null;
}
