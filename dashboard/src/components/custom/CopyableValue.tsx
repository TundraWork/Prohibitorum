import { useLingui } from "@lingui/react/macro";
import { CheckIcon, CircleAlertIcon, CopyIcon } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import { Button } from "@/components/custom/Button";
import { useCopyValue } from "@/components/custom/use-copy-value";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

/**
 * The value with a break point after each `/` of its path and each `?` and `&`
 * of its query, so an address wraps between its parts rather than inside one;
 * never after the scheme's `//`, which would leave "https://" alone on a line.
 * `overflow-wrap: anywhere` on the caller's text is still the fallback for a
 * part longer than the line. The break points are empty elements, so the
 * text, a selection and a copy are the value itself.
 *
 * `atomic` also keeps each part whole: a part that fits on the line is never
 * broken at a hyphen inside it, and one longer than the line fills each line
 * to the edge rather than breaking at its own hyphens, which matters for a
 * token a reader may copy out by hand.
 */
export function breakAtSlashes(
  value: string,
  { atomic = false }: { atomic?: boolean } = {},
): ReactNode {
  return value.split(/(?<=[^/]\/|[?&])(?!\/)/).map((part, index) => (
    // The segments are positional and never reordered.
    // biome-ignore lint/suspicious/noArrayIndexKey: see above
    <Fragment key={index}>
      {index > 0 && <wbr />}
      {atomic ? (
        <span className="inline-block max-w-full break-all">{part}</span>
      ) : (
        part
      )}
    </Fragment>
  ));
}

/**
 * A short machine value shown inline in monospace with a button that copies it:
 * the icon turns into a check, a hidden status says "Copied", and a refused
 * clipboard shows a warning. For an identifier in a header or a side rail; a
 * field the reader pastes from is `CopyValue`.
 */
export function CopyableValue({
  value,
  copyValue,
  label,
  wrap = false,
  className,
}: {
  value: string;
  /** What the button copies, when that is not the text shown. */
  copyValue?: string;
  /** Names the copy button, such as "Copy the client ID". */
  label: string;
  /** Lets the value wrap, after its slashes; otherwise give it `truncate`. */
  wrap?: boolean;
  /** Classes for the value's text, such as its size or truncation. */
  className?: string;
}): React.ReactElement {
  const { state, copy } = useCopyValue();
  const copied = state === "copied";
  return (
    // The button lines up with the value's first line, so a value that wraps
    // keeps its control where the eye starts reading.
    <span className="inline-flex min-w-0 items-start gap-1">
      <span
        className={cn(
          "min-w-0 py-1.5 font-mono text-foreground",
          wrap && "wrap-anywhere",
          className,
        )}
      >
        {wrap ? breakAtSlashes(value) : value}
      </span>
      <Button
        aria-label={label}
        onClick={() => copy(copyValue ?? value)}
        variant="ghost"
        size="icon-sm"
      >
        {copied ? (
          <CheckIcon aria-hidden="true" className="text-success" />
        ) : (
          <CopyIcon aria-hidden="true" />
        )}
      </Button>
      <CopyStatus state={state} />
    </span>
  );
}

/**
 * What a copy button says for itself: a hidden "Copied" status while the
 * clipboard took the value, and a warning when it refused.
 *
 * The warning points at "the value above", so it belongs directly under what
 * was to be copied. Every copy control on the console draws it from here, so
 * the wording cannot differ between them.
 */
export function CopyStatus({
  state,
  failedMessage,
}: {
  state: "idle" | "copied" | "failed";
  /** A caller's own warning, for a value with its own way out such as a download. */
  failedMessage?: string;
}): React.ReactElement {
  const { t } = useLingui();
  return (
    <>
      <span role="status" className="sr-only">
        {state === "copied"
          ? t({ id: "copy-value.copied", message: "Copied" })
          : ""}
      </span>
      {state === "failed" && (
        <Alert variant="warning">
          <CircleAlertIcon aria-hidden="true" />
          <AlertTitle>
            {failedMessage ??
              t({
                id: "copy-value.failed",
                message:
                  "Could not copy. Select the value above and copy it manually.",
              })}
          </AlertTitle>
        </Alert>
      )}
    </>
  );
}
