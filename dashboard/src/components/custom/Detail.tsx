import type { ReactNode } from "react";

/** One labelled value in a `dl`, such as the facts a dialog asks the reader to check. */
export function Detail({
  label,
  children,
}: {
  label: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-sm text-muted">{label}</dt>
      <dd className="wrap-anywhere">{children}</dd>
    </div>
  );
}
