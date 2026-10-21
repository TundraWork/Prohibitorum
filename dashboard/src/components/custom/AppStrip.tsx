import type { ReactNode } from "react";
import { EntityAvatar } from "@/components/custom/EntityAvatar";

/**
 * The application a public page is about, drawn in the public stage's header
 * strip: its tile, a lead line saying what is happening, its name and host.
 * Every page a sign-in to an application passes through draws it the same
 * way, so the reader can tell at a glance where they are going.
 *
 * It is the first thing on the stage and sized to be read as such: a 52px
 * tile and the name one step under the card's heading. The tile shows the
 * application's uploaded icon and falls back to the name's initials; a
 * truncated name keeps its full text in `title`. On a frosted frame the lead
 * and host take the full text colour, as muted text fails contrast over a
 * picture.
 */
export function AppStrip({
  lead,
  name,
  host,
  iconUrl,
}: {
  lead: ReactNode;
  name: string;
  host?: string | undefined;
  iconUrl?: string | undefined;
}) {
  return (
    <div className="flex min-w-0 items-center gap-3.5 py-1.5">
      <EntityAvatar
        iconUrl={iconUrl}
        name={name}
        className="size-13 shrink-0 rounded-[14px] bg-background text-lg shadow-xs/5 ring-1 ring-black/8 dark:ring-white/10"
        fallbackClassName="bg-background"
      />
      <div className="min-w-0 leading-snug">
        <div className="text-foreground/75 text-sm in-data-[frame=frosted]:text-foreground">
          {lead}
        </div>
        <div
          className="truncate font-semibold text-xl tracking-tight"
          title={name}
        >
          {name}
        </div>
        {host !== undefined && host !== "" && (
          <div className="truncate font-mono text-muted-foreground text-xs in-data-[frame=frosted]:text-foreground">
            {host}
          </div>
        )}
      </div>
    </div>
  );
}
