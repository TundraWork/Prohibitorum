import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// `text-caption` is a font size from the theme; without this, merging would
// read it as a text colour and drop the colour class beside it.
const twMerge = extendTailwindMerge({
  extend: { theme: { text: ["caption"] } },
});

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
