import type { GiName } from "@/lib/gameicons";

/**
 * Icon key come from the backend catalogue (internal/achievements); unknown
 * keys fall back to the trophy so a new achievement still renders.
 */
const ICONS: Record<string, GiName> = {
  droplet: "droplet",
  brush: "brush",
  "broom": "broom",
  "bubbles": "bubbles",
  "gas-mask": "gas-mask",
  "small-fire": "small-fire",
  "chevrons-down": "chevrons-down",
  "hammer-drop": "hammer-drop",
  tooth: "tooth",
  "meteor-impact": "meteor-impact",
  "wooden-door": "wooden-door",
  minus: "minus",
  shovel: "shovel",
  timer: "timer",
  mountain: "mountain",
  target: "target",
  "door-open": "door-open",
  hourglass: "hourglass",
  amphora: "amphora",
  "mayan-pyramid": "mayan-pyramid",
  "stone-tablet": "stone-tablet",
  "time-trap": "time-trap",
  "vintage-robot": "vintage-robot",
  fossil: "fossil",
  "lightning-storm": "lightning-storm",
  bookshelf: "bookshelf",
  // The generic glyph a locked hidden achievement is masked with.
  lock: "lock",
};

export function achievementIcon(key: string): GiName {
  return ICONS[key] ?? "trophy";
}
