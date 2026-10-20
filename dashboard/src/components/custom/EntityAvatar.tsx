import { AppWindow } from "lucide-react";
import type { ReactNode } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { cn } from "@/lib/utils";

const sizeClass = {
  sm: "size-6 text-[10px]",
  md: "size-8 text-xs",
  lg: "size-12 text-base",
  xl: "size-14 text-lg",
} as const;

/**
 * Letters for the placeholder: the first two words' initials, or one letter. A
 * bracketed aside such as "(dev)" and punctuation are not words, so "Prometheus
 * (dev)" is "P" rather than "P(".
 */
export function initialsOf(name: string): string {
  const wordsOf = (text: string) => text.match(/[\p{L}\p{N}]+/gu) ?? [];
  const main = wordsOf(name.replace(/\([^)]*\)|\[[^\]]*\]/g, " "));
  const words = main.length > 0 ? main : wordsOf(name);
  const letters = words.slice(0, 2).map((word) => Array.from(word)[0] ?? "");
  return letters.join("").toLocaleUpperCase();
}

/**
 * The icon of one entity — an application, an identity provider, an account —
 * as it appears in a list row and on the detail page's icon card.
 *
 * Every list on the console draws this the same way: the entity's own icon when
 * it has one, and otherwise a neutral placeholder at the same size, so a column
 * of rows scans as a column and no row looks unfinished. Applications and
 * providers are rounded tiles and people are round; the rounding and the steps
 * of size are fixed here rather than at each call site, because a list row and
 * an icon card showing the same entity must not disagree.
 */
export function EntityAvatar({
  iconUrl,
  name,
  fallback,
  size = "md",
  shape = "tile",
  className,
  fallbackClassName,
}: {
  iconUrl?: string | undefined;
  /** Source of the placeholder letters when there is no icon. */
  name?: string | undefined;
  /** Shown when there is no icon; wins over the letters from `name`. */
  fallback?: ReactNode;
  /** 24, 32, 48 or 56 pixels. */
  size?: keyof typeof sizeClass;
  shape?: "tile" | "round";
  /** Overrides the size where a call site needs a step the scale lacks. */
  className?: string;
  /** Extra classes for the placeholder tile. */
  fallbackClassName?: string;
}) {
  const initials = name === undefined ? "" : initialsOf(name);
  const rounding = shape === "round" ? "rounded-full" : "rounded-lg";
  return (
    <Avatar
      data-shape={shape}
      className={cn(sizeClass[size], rounding, className)}
    >
      {iconUrl !== undefined && iconUrl !== "" && (
        <AvatarImage src={iconUrl} alt="" />
      )}
      <AvatarFallback className={cn(rounding, fallbackClassName)}>
        {fallback ??
          (initials === "" ? (
            <AppWindow className="size-4" aria-hidden="true" />
          ) : (
            initials
          ))}
      </AvatarFallback>
    </Avatar>
  );
}
