import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** Merges Tailwind class lists, resolving conflicting utilities in favor of
 * the later one (shadcn/ui's standard `cn` helper). Every component in
 * `components/ui` composes classes through this rather than string
 * concatenation, so consumer overrides behave predictably. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
