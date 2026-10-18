import type { ReactNode } from "react";

/** One setting: its name and explanation on the left, its control at the end. */
export function SettingRow({
  title,
  description,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}): React.ReactElement {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 py-3.5 first:pt-0 last:pb-0">
      <div>
        <div className="font-medium text-sm">{title}</div>
        {description !== undefined && (
          <div className="text-muted-foreground text-sm">{description}</div>
        )}
      </div>
      {children}
    </div>
  );
}
