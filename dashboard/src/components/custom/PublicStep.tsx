import { cva } from "class-variance-authority";
import { ArrowLeft } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";
import { PageHeading } from "@/components/custom/PageHeading";

/**
 * One page on the public card: what it is about, its question as the title,
 * the facts that answer it, then the decision. Every public page — sign-in,
 * consent, a failed flow, maintenance — draws its title row here, so the
 * heading, its focus and the spacing are the same on all of them.
 */
function PublicStepRoot({
  media,
  title,
  titleKey,
  back,
  description,
  children,
  actions,
}: {
  /** The entity the page is about, above the title: an application's icon. */
  media?: ReactNode;
  title: ReactNode;
  /** A new value remounts the title, which takes focus again. */
  titleKey?: string;
  back?: { label: string; onPress: () => void; isDisabled?: boolean };
  description?: ReactNode;
  children?: ReactNode;
  /** The page's buttons, as a `PublicStep.Actions`. */
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4">
      {media}
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {back && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              isIconOnly
              className="shrink-0"
              aria-label={back.label}
              isDisabled={back.isDisabled}
              onPress={back.onPress}
            >
              <ArrowLeft aria-hidden="true" />
            </Button>
          )}
          <PageHeading key={titleKey}>{title}</PageHeading>
        </div>
        {description && <p className="text-sm text-muted">{description}</p>}
      </div>
      {children}
      {actions}
    </div>
  );
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
  return <div className={actionsLayout({ layout })}>{children}</div>;
}

export const PublicStep = Object.assign(PublicStepRoot, {
  Actions: PublicStepActions,
});
