import { useLingui } from "@lingui/react/macro";
import type { ReactNode } from "react";
import { CopyableValue } from "@/components/custom/CopyableValue";
import { Badge } from "@/components/ui/badge";

export type ObjectStatus =
  | "enabled"
  | "disabled"
  | "restricted"
  | "open"
  | "sign-in-on"
  | "sign-in-off"
  | "not-ready"
  | "offered"
  | "not-offered";

interface ObjectHeaderProps {
  /** The entity's icon tile. */
  icon: ReactNode;
  /** Rendered as the page's `h1`. */
  title: ReactNode;
  statuses?: ObjectStatus[];
  /** A short machine name, shown in mono with a copy button named by `label`. */
  identifier?: { value: string; label: string };
  /** Short facts, separated by hairlines. */
  facts?: ReactNode[];
  /** Aligned to the end. */
  actions?: ReactNode;
}

/**
 * The top of an object page: icon, title, status badges, an identifier that
 * copies, a line of facts and the page's actions.
 *
 * The hairlines between the facts are each item's own leading edge, pushed
 * out of sight by the clipping row when the item starts a line. A separator
 * drawn between items would be left dangling at the end of a line the facts
 * wrapped on.
 *
 * An identifier that wraps breaks after its slashes. Below `sm` the actions
 * sit left under the text, as a `PageHeader`'s do, and take the trailing edge
 * from `sm`.
 */
export function ObjectHeader({
  icon,
  title,
  statuses = [],
  identifier,
  facts = [],
  actions,
}: ObjectHeaderProps): React.ReactElement {
  const { t } = useLingui();

  const statusText: Record<ObjectStatus, string> = {
    enabled: t({ id: "object.status.enabled", message: "Enabled" }),
    disabled: t({ id: "object.status.disabled", message: "Disabled" }),
    restricted: t({ id: "object.status.restricted", message: "Restricted" }),
    open: t({ id: "object.status.open", message: "Open" }),
    "sign-in-on": t({ id: "object.status.sign-in-on", message: "Sign-in on" }),
    "sign-in-off": t({
      id: "object.status.sign-in-off",
      message: "Sign-in off",
    }),
    "not-ready": t({ id: "object.status.not-ready", message: "Not ready" }),
    offered: t({
      id: "object.status.offered",
      message: "Offered to applications",
    }),
    "not-offered": t({
      id: "object.status.not-offered",
      message: "Not offered",
    }),
  };
  const statusVariant: Record<ObjectStatus, "success" | "outline" | "warning"> =
    {
      enabled: "success",
      disabled: "outline",
      restricted: "warning",
      open: "outline",
      "sign-in-on": "outline",
      "sign-in-off": "outline",
      "not-ready": "warning",
      offered: "outline",
      "not-offered": "outline",
    };

  const meta: ReactNode[] = [];
  if (statuses.length > 0) {
    meta.push(
      <ul
        key="statuses"
        aria-label={t({ id: "object.status.label", message: "Status" })}
        className="flex flex-wrap items-center gap-2"
      >
        {statuses.map((status) => (
          <li key={status}>
            <Badge variant={statusVariant[status]}>
              {(status === "enabled" || status === "not-ready") && (
                <span
                  aria-hidden="true"
                  className="size-1.5 rounded-full bg-current"
                />
              )}
              {statusText[status]}
            </Badge>
          </li>
        ))}
      </ul>,
    );
  }
  if (identifier !== undefined) {
    meta.push(
      <CopyableValue
        key="identifier"
        value={identifier.value}
        label={identifier.label}
        wrap
        className="wrap-anywhere text-[13px]"
      />,
    );
  }
  facts.forEach((fact, index) => {
    // Facts are caller-ordered and static, so the index is a stable key.
    // biome-ignore lint/suspicious/noArrayIndexKey: see above
    meta.push(<span key={`fact-${index}`}>{fact}</span>);
  });

  return (
    <div
      data-slot="object-header"
      className="flex flex-wrap items-center gap-x-4 gap-y-3"
    >
      {icon}
      <div className="min-w-0 flex-1 basis-56">
        <h1 className="font-semibold text-2xl tracking-tight">{title}</h1>
        {meta.length > 0 && (
          // Padded and pulled back by the same amount, so the clip does not
          // cut the copy button's focus ring where it starts a line.
          <div className="-mx-1 overflow-hidden p-1">
            <div className="-ms-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-muted-foreground text-sm">
              {meta.map((item) => (
                <div
                  key={(item as React.ReactElement).key}
                  className="relative flex items-center ps-3 before:absolute before:start-0 before:top-1/2 before:h-4 before:w-px before:-translate-y-1/2 before:bg-border"
                >
                  {item}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
      {actions !== undefined && (
        <div className="flex gap-2 sm:ms-auto">{actions}</div>
      )}
    </div>
  );
}
