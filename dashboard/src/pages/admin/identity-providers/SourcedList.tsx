import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";

export interface SourcedRow {
  key: string;
  label: ReactNode;
  value: ReactNode;
  /** Absent when the value has no source to name. */
  source?: ReactNode;
  /** The value is one an operator changed, so its source stands out. */
  changed?: boolean;
}

/**
 * A list of what a value is, the value, and where it came from, shared by both
 * diagnostics so they read alike. It switches on its own width: wide, the three
 * line up across the dialog; narrow, the name and its source share a line over
 * the value, which is the column that needs the room.
 */
export function SourcedList({ rows }: { rows: readonly SourcedRow[] }) {
  return (
    <div className="@container/sourced">
      <dl className="flex flex-col divide-y">
        {rows.map((row) => (
          <div
            key={row.key}
            data-field={row.key}
            className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 gap-y-1 py-2.5 first:pt-0 last:pb-0 @xl/sourced:grid-cols-[9rem_minmax(0,1fr)_minmax(0,auto)] @xl/sourced:gap-x-6"
          >
            <dt className="text-muted-foreground text-sm">{row.label}</dt>
            <dd className="col-span-2 row-start-2 min-w-0 @xl/sourced:col-span-1 @xl/sourced:col-start-2 @xl/sourced:row-start-1">
              {row.value}
            </dd>
            <dd className="col-start-2 row-start-1 min-w-0 justify-self-end @xl/sourced:col-start-3">
              {row.source !== undefined && row.source !== null && (
                <Badge
                  variant={row.changed === true ? "info" : "outline"}
                  data-changed={row.changed === true ? "" : undefined}
                  className="h-auto min-h-5 max-w-full whitespace-normal wrap-anywhere text-start sm:min-h-4.5"
                >
                  {row.source}
                </Badge>
              )}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
