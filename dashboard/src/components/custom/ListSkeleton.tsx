import { Skeleton } from "@heroui/react";

/**
 * The rows a list is about to hold, drawn while its read is in flight.
 *
 * Every list on the console waits with this: `ItemList` uses it for its own
 * loading state, and a panel that reads with `useQuery` — which cannot suspend,
 * so it shows its own pending state — uses it to keep the block the same shape
 * whether or not the rows have arrived. Drawing it once means the wait and the
 * result cannot disagree about a row's height or about where its icon sits, and
 * the page does not jump when the data lands.
 *
 * Rows are a fixed height rather than the real content's: this stands in for a
 * row's anatomy — icon, name, detail line — and never for its text.
 *
 * It is decorative, and carries no accessible name of its own. The list's own
 * label is what a reader is waiting on, and a set of placeholder bars has
 * nothing to announce; the container takes it out of the accessibility tree so
 * that a reader is not told about shape without content.
 *
 * `relative overflow-hidden` are not optional alongside `skeleton--shimmer`.
 * The shimmer is an absolutely positioned pseudo-element that crosses a box by
 * translating itself a full width and more, so without a positioned ancestor it
 * resolves against the viewport and its band sweeps the whole page — over the
 * console's rail. Without `overflow-hidden` nothing clips it back to the box
 * either, and the sweep is wider than that box to begin with.
 */
export function ListSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div
      aria-hidden="true"
      className="skeleton--shimmer relative flex flex-col gap-4 overflow-hidden p-4"
    >
      {Array.from({ length: rows }).map((_, index) => (
        // Placeholders are positional and never reordered.
        // biome-ignore lint/suspicious/noArrayIndexKey: see above
        <div key={index} className="flex items-center gap-3">
          <Skeleton animationType="none" className="size-9 shrink-0" />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Skeleton animationType="none" className="h-3.5 w-1/3" />
            <Skeleton animationType="none" className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  );
}
