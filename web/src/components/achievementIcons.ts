import { Trophy, type LucideIcon } from "lucide-react";

import {
  Brush,
  DoorOpen,
  Droplet,
  Hourglass,
  Mountain,
  Shovel,
  Target,
  Timer,
} from "lucide-react";

/**
 * Icon keys come from the backend catalogue (internal/achievements); unknown
 * keys fall back to the trophy so a new achievement still renders.
 */
const ICONS: Record<string, LucideIcon> = {
  droplet: Droplet,
  brush: Brush,
  shovel: Shovel,
  timer: Timer,
  mountain: Mountain,
  target: Target,
  "door-open": DoorOpen,
  hourglass: Hourglass,
};

export function achievementIcon(key: string): LucideIcon {
  return ICONS[key] ?? Trophy;
}
