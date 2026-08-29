import type { GiName } from "@/lib/gameicons";

/**
 * Icon key come from the backend catalogue (internal/achievements); unknown
 * keys fall back to the trophy so a new achievement still renders.
 */
const ICONS: Record<string, GiName> = {
  droplet: "droplet",
  brush: "brush",
  shovel: "shovel",
  timer: "timer",
  mountain: "mountain",
  target: "target",
  "door-open": "door-open",
  hourglass: "hourglass",
  // The generic glyph a locked hidden achievement is masked with.
  lock: "lock",
};

export function achievementIcon(key: string): GiName {
  return ICONS[key] ?? "trophy";
}
