import { useLingui } from "@lingui/react/macro";
import { CheckIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * Where the reader is in a short run of steps: a numbered dot and a name for
 * each, a check once a step is done, and the current one ringed. The steps
 * share the width equally, so a name too long for its column wraps inside it.
 *
 * `current` is the zero-based index of the step on screen.
 */
export function StepHeader({
  steps,
  current,
  label,
}: {
  steps: ReactNode[];
  current: number;
  /** Names the list for assistive technology. */
  label: string;
}) {
  const { t } = useLingui();
  return (
    <ol aria-label={label} className="grid auto-cols-fr grid-flow-col gap-2">
      {steps.map((step, index) => {
        const done = index < current;
        const active = index === current;
        return (
          <li
            // The steps are fixed for the life of the list.
            // biome-ignore lint/suspicious/noArrayIndexKey: positions are the identity
            key={index}
            aria-current={active ? "step" : undefined}
            className="flex min-w-0 flex-col gap-1.5 text-xs"
          >
            <span className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className={cn(
                  "flex size-5 shrink-0 items-center justify-center rounded-full font-medium tabular-nums",
                  done && "bg-primary text-primary-foreground",
                  active && "text-foreground ring-1 ring-primary",
                  !done && !active && "bg-muted text-muted-foreground",
                )}
              >
                {done ? <CheckIcon className="size-3" /> : index + 1}
              </span>
              {index < steps.length - 1 && (
                <span aria-hidden="true" className="h-px flex-1 bg-border" />
              )}
            </span>
            {done && (
              <span className="sr-only">
                {t({ id: "step-header.done", message: "Completed" })}
              </span>
            )}
            <span
              className={
                active ? "font-medium text-foreground" : "text-muted-foreground"
              }
            >
              {step}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
