import { useLingui } from "@lingui/react/macro";
import { CheckIcon, CopyIcon } from "lucide-react";
import { Fragment, type ReactNode, useEffect, useRef, useState } from "react";
import { Button } from "@/components/custom/Button";
import { Badge } from "@/components/ui/badge";

export type ObjectStatus =
  | "enabled"
  | "disabled"
  | "restricted"
  | "open"
  | "sign-in-on"
  | "sign-in-off";

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

const COPIED_MS = 2000;

function Separator(): React.ReactElement {
  return (
    <span aria-hidden="true" className="text-border">
      |
    </span>
  );
}

/**
 * The top of an object page: icon, title, status badges, an identifier that
 * copies, a line of facts and the page's actions.
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
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

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
  };
  const statusVariant: Record<ObjectStatus, "success" | "outline" | "warning"> =
    {
      enabled: "success",
      disabled: "outline",
      restricted: "warning",
      open: "outline",
      "sign-in-on": "outline",
      "sign-in-off": "outline",
    };

  const copy = () => {
    if (identifier === undefined) return;
    navigator.clipboard.writeText(identifier.value).then(
      () => {
        clearTimeout(timer.current);
        setCopied(true);
        timer.current = setTimeout(() => setCopied(false), COPIED_MS);
      },
      () => {},
    );
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
              {status === "enabled" && (
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
      <span key="identifier" className="inline-flex items-center gap-1">
        <span className="font-mono text-[13px] text-foreground">
          {identifier.value}
        </span>
        <Button
          variant="ghost"
          size="sm"
          isIconOnly
          aria-label={identifier.label}
          onPress={copy}
        >
          {copied ? (
            <CheckIcon aria-hidden="true" className="text-success" />
          ) : (
            <CopyIcon aria-hidden="true" />
          )}
        </Button>
        <span role="status" className="sr-only">
          {copied ? t({ id: "copy-value.copied", message: "Copied" }) : ""}
        </span>
      </span>,
    );
  }
  facts.forEach((fact, index) => {
    // Facts are caller-ordered and static, so the index is a stable key.
    // biome-ignore lint/suspicious/noArrayIndexKey: see above
    meta.push(<span key={`fact-${index}`}>{fact}</span>);
  });

  return (
    <div className="flex items-center gap-4">
      {icon}
      <div className="min-w-0">
        <h1 className="font-semibold text-2xl tracking-tight">{title}</h1>
        {meta.length > 0 && (
          <div className="mt-1 flex flex-wrap items-center gap-2 text-muted-foreground text-sm">
            {meta.map((item, index) => (
              <Fragment key={(item as React.ReactElement).key}>
                {index > 0 && <Separator />}
                {item}
              </Fragment>
            ))}
          </div>
        )}
      </div>
      {actions !== undefined && (
        <div className="ms-auto flex gap-2">{actions}</div>
      )}
    </div>
  );
}
