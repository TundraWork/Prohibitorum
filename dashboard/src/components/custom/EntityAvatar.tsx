import { Avatar } from "@heroui/react";
import { AppWindow } from "lucide-react";
import type { ReactNode } from "react";

/**
 * The icon of one entity — an application, an identity provider, an account —
 * as it appears in a list row and on the detail page's icon card.
 *
 * Every list on the console draws this the same way: the entity's own icon when
 * it has one, and otherwise a neutral placeholder at the same size, so a column
 * of rows scans as a column and no row looks unfinished. The rounding and the
 * size are fixed here rather than at each call site, because a list row and an
 * icon card showing the same entity must not disagree.
 */
export function EntityAvatar({
  iconUrl,
  /** Shown when there is no icon; the entity's first letter by default. */
  fallback,
  className = "size-9",
}: {
  iconUrl?: string | undefined;
  fallback?: ReactNode;
  /** Sets the size; the rounding is this component's own. */
  className?: string;
}) {
  return (
    <Avatar className={`${className} rounded-[0.375rem]`}>
      {iconUrl !== undefined && iconUrl !== "" && (
        <Avatar.Image src={iconUrl} alt="" />
      )}
      <Avatar.Fallback className="rounded-[0.375rem]">
        {fallback ?? <AppWindow size={18} aria-hidden="true" />}
      </Avatar.Fallback>
    </Avatar>
  );
}
