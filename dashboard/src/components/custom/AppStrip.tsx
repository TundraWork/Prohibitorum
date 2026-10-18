import type { ReactNode } from "react";
import { EntityAvatar } from "@/components/custom/EntityAvatar";

/**
 * The application a public page is about, drawn in the public stage's header
 * strip: its tile, a lead line saying what is happening, its name and host.
 *
 * The name is a label the server chose, so the tile falls back to the name's
 * initials; a truncated name keeps its full text in `title`. On a frosted frame
 * the lead and host take the full text colour, as muted text fails contrast
 * over a picture.
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
    <>
      <EntityAvatar
        iconUrl={iconUrl}
        name={name}
        className="size-10 rounded-xl bg-background text-base shadow-xs/5 ring-1 ring-black/8 dark:ring-white/10"
        fallbackClassName="bg-background"
      />
      <div className="min-w-0 leading-tight">
        <div className="text-muted-foreground text-xs in-data-[frame=frosted]:text-foreground">
          {lead}
        </div>
        <div className="truncate font-semibold text-base" title={name}>
          {name}
        </div>
        {host !== undefined && host !== "" && (
          <div className="truncate font-mono text-muted-foreground text-xs in-data-[frame=frosted]:text-foreground">
            {host}
          </div>
        )}
      </div>
    </>
  );
}
