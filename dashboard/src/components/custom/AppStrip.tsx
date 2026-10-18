import type { ReactNode } from "react";
import { EntityAvatar } from "@/components/custom/EntityAvatar";

/**
 * The application a public page is about, drawn in the public stage's header
 * strip: its tile, a lead line saying what is happening, its name and host.
 *
 * The name is a label the server chose, so the tile falls back to the name's
 * initials; a truncated name keeps its full text in `title`.
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
        className="size-10 rounded-xl text-base"
      />
      <div className="min-w-0 leading-tight">
        <div className="text-muted-foreground text-xs">{lead}</div>
        <div className="truncate font-semibold text-base" title={name}>
          {name}
        </div>
        {host !== undefined && host !== "" && (
          <div className="truncate font-mono text-muted-foreground text-xs">
            {host}
          </div>
        )}
      </div>
    </>
  );
}
