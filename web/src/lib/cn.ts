import clsx, { type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Merges class names, resolving Tailwind conflicts in favour of the last one.
 *
 * Plain clsx only concatenates, so a component's own `w-full` and a caller's
 * `w-11` both survive and the winner is decided by stylesheet order rather than
 * by intent — which silently breaks component sizing. twMerge drops the earlier
 * class in each conflicting group, so caller overrides actually win.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
