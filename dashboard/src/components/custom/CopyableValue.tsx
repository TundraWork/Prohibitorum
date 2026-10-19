import { useLingui } from "@lingui/react/macro";
import { CheckIcon, CircleAlertIcon, CopyIcon } from "lucide-react";
import { Button } from "@/components/custom/Button";
import { useCopyValue } from "@/components/custom/use-copy-value";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

/**
 * A short machine value shown inline in monospace with a button that copies it:
 * the icon turns into a check, a hidden status says "Copied", and a refused
 * clipboard shows a warning. For an identifier in a header or a side rail; a
 * field the reader pastes from is `CopyValue`.
 */
export function CopyableValue({
  value,
  label,
  className,
}: {
  value: string;
  /** Names the copy button, such as "Copy the client ID". */
  label: string;
  /** Classes for the value's text, such as its size or truncation. */
  className?: string;
}): React.ReactElement {
  const { t } = useLingui();
  const { state, copy } = useCopyValue();
  const copied = state === "copied";
  return (
    <span className="inline-flex min-w-0 items-center gap-1">
      <span className={cn("min-w-0 font-mono text-foreground", className)}>
        {value}
      </span>
      <Button
        aria-label={label}
        onClick={() => copy(value)}
        variant="ghost"
        size="icon-sm"
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
      {state === "failed" && (
        <Alert variant="warning">
          <CircleAlertIcon aria-hidden="true" />
          <AlertTitle>
            {t({
              id: "copy-value.failed",
              message:
                "Couldn't copy. Select the value above and copy it yourself.",
            })}
          </AlertTitle>
        </Alert>
      )}
    </span>
  );
}
