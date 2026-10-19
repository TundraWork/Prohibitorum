import type { ReactNode } from "react";

/**
 * One setting: its name and explanation on the left, its control at the end.
 * Below the `sm` breakpoint the control drops under the text, left-aligned at its natural width, as an `ItemListRow` with `stackActions` does.
 */
export function SettingRow({
  title,
  description,
  descriptionId,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  /** Lets the control name its explanation with `aria-describedby`. */
  descriptionId?: string;
  children: ReactNode;
}): React.ReactElement {
  return (
    <div className="grid grid-cols-1 items-center sm:grid-cols-[minmax(0,1fr)_auto] gap-4 py-3.5 first:pt-0 last:pb-0">
      <div>
        <div className="font-medium text-sm">{title}</div>
        {description !== undefined && (
          <div id={descriptionId} className="text-muted-foreground text-sm">
            {description}
          </div>
        )}
      </div>
      <div className="flex items-center sm:justify-end">{children}</div>
    </div>
  );
}
