import { cva } from "class-variance-authority";
import { ArrowLeft } from "lucide-react";
import { type ReactNode, useEffect } from "react";
import { createPortal } from "react-dom";
import { Button } from "@/components/custom/Button";
import { PageHeading } from "@/components/custom/PageHeading";
import { useStageSlots } from "@/components/custom/PublicStage";

/**
 * One page on the public card: what it is about, its question as the title,
 * the facts that answer it, then the decision. Every public page — sign-in,
 * consent, a failed flow, maintenance — draws its title row here, so the
 * heading, its focus and the spacing are the same on all of them.
 */
function PublicStepRoot({
  title,
  titleKey,
  back,
  description,
  strip,
  footer,
  wide = false,
  children,
  actions,
}: {
  title: ReactNode;
  /** A new value remounts the title, which takes focus again. */
  titleKey?: string;
  back?: { label: string; onClick: () => void; disabled?: boolean };
  description?: ReactNode;
  /** Drawn in the stage's strip above the card: who is asking. */
  strip?: ReactNode;
  /** Drawn in the stage's footer under the card: a choice or the decision. */
  footer?: ReactNode;
  /** Asks the stage for its wider card while this page is shown. */
  wide?: boolean;
  children?: ReactNode;
  /** The page's buttons, as a `PublicStep.Actions`. */
  actions?: ReactNode;
}) {
  const slots = useStageSlots();
  const { setWidth } = slots;
  useEffect(() => {
    if (!wide) return;
    setWidth("wide");
    return () => setWidth("default");
  }, [wide, setWidth]);

  return (
    <div className="flex flex-col gap-4">
      <Around slot={slots.strip} inStage={slots.inStage}>
        {strip}
      </Around>
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {back && (
            <Button
              type="button"
              aria-label={back.label}
              disabled={back.disabled}
              onClick={back.onClick}
              variant="ghost"
              size="icon-sm"
              className="shrink-0"
            >
              <ArrowLeft aria-hidden="true" />
            </Button>
          )}
          <PageHeading key={titleKey}>{title}</PageHeading>
        </div>
        {description && (
          <p className="text-muted-foreground text-sm">{description}</p>
        )}
      </div>
      {children}
      {actions}
      <Around slot={slots.footer} inStage={slots.inStage}>
        {footer}
      </Around>
    </div>
  );
}

/**
 * Puts content into one of the stage's regions around the card. Outside a
 * stage there is no region, so it is drawn in place; inside one, it waits for
 * the region to mount.
 */
function Around({
  slot,
  inStage,
  children,
}: {
  slot: HTMLElement | null;
  inStage: boolean;
  children: ReactNode;
}) {
  if (!children) return null;
  if (!inStage) return <>{children}</>;
  return slot ? createPortal(children, slot) : null;
}

const actionsLayout = cva("", {
  variants: {
    layout: {
      // One way forward, then the lesser ones under it, all full width. A
      // label too long for a narrow card wraps inside its button.
      single:
        "flex flex-col gap-2 [&_button]:h-auto [&_button]:min-h-10 [&_button]:whitespace-normal [&_button]:py-2 md:[&_button]:min-h-9",
      // Two equal choices side by side, the one that goes ahead on the right
      // as in a dialog's footer. The card is never too narrow for two.
      decision: "grid grid-cols-2 gap-2",
    },
  },
  defaultVariants: { layout: "single" },
});

function PublicStepActions({
  layout,
  children,
}: {
  layout?: "single" | "decision";
  children: ReactNode;
}) {
  return (
    <div data-slot="step-actions" className={actionsLayout({ layout })}>
      {children}
    </div>
  );
}

export const PublicStep = Object.assign(PublicStepRoot, {
  Actions: PublicStepActions,
});
