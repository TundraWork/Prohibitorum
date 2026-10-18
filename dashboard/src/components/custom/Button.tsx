import * as React from "react";
import { Button as AriaButton } from "react-aria-components";
import { buttonVariants, Button as CossButton } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

type ButtonVariant =
  | "primary"
  | "secondary"
  | "tertiary"
  | "ghost"
  | "outline"
  | "danger"
  | "danger-soft"
  | "warning";

type ButtonSize = "sm" | "md" | "lg";

type CossVariant = NonNullable<
  React.ComponentProps<typeof CossButton>["variant"]
>;

export interface ButtonProps
  extends Omit<
    React.ComponentProps<"button">,
    "onClick" | "disabled" | "slot" | "tabIndex" | "title" | "role"
  > {
  /** Keeps the button out of the tab order (renders tabindex="-1"). */
  excludeFromTabOrder?: boolean;
  /** Pressing the button does not move focus to it. */
  preventFocusOnPress?: boolean;
  variant?: ButtonVariant;
  size?: ButtonSize;
  isIconOnly?: boolean;
  isPending?: boolean;
  isDisabled?: boolean;
  fullWidth?: boolean;
  onPress?: (event: React.MouseEvent<HTMLButtonElement>) => void;
  onClick?: (event: React.MouseEvent<HTMLButtonElement>) => void;
  render?: React.ReactElement;
  /** Names the react-aria slot a parent (e.g. a table) assigns this button to. */
  slot?: string | null;
}

// `warning` keeps the primary look: the consequence of a save is stated by the
// section footer, not by the button's colour.
const cossVariant: Record<ButtonVariant, CossVariant> = {
  primary: "default",
  secondary: "outline",
  outline: "outline",
  tertiary: "secondary",
  ghost: "ghost",
  danger: "destructive",
  "danger-soft": "destructive-outline",
  warning: "default",
};

const cossSize = {
  sm: { text: "sm", icon: "icon-sm" },
  md: { text: "default", icon: "icon" },
  lg: { text: "lg", icon: "icon-lg" },
} as const;

/**
 * coss's button with the console's prop vocabulary (`onPress`, `isPending`,
 * `isDisabled`, ...) so screens keep one spelling.
 *
 * A pending button with text keeps that text beside the spinner; an icon-only
 * one has room for a single glyph, so the spinner takes the icon's place.
 */
export function Button({
  variant = "primary",
  size = "md",
  isIconOnly = false,
  isPending = false,
  isDisabled = false,
  fullWidth = false,
  onPress,
  onClick,
  render,
  slot,
  excludeFromTabOrder,
  preventFocusOnPress,
  ref,
  className,
  children,
  ...props
}: ButtonProps): React.ReactElement {
  const common = {
    variant: cossVariant[variant],
    size: cossSize[size][isIconOnly ? "icon" : "text"],
  } as const;
  const content = (
    <>
      {isPending && <Spinner aria-hidden="true" data-slot="spinner" />}
      {isPending && isIconOnly ? null : children}
    </>
  );
  const handleClick = (event: React.MouseEvent) => {
    if (isPending) {
      event.preventDefault();
      return;
    }
    const e = event as React.MouseEvent<HTMLButtonElement>;
    onClick?.(e);
    onPress?.(e);
  };
  const inner = React.useRef<HTMLButtonElement>(null);
  // react-aria's button drops aria-busy from its props, so set it directly.
  React.useLayoutEffect(() => {
    if (isPending) inner.current?.setAttribute("aria-busy", "true");
    else inner.current?.removeAttribute("aria-busy");
  }, [isPending]);
  const marks = {
    "data-variant": variant,
    "data-size": size,
  };

  // Links are never trigger children, so they keep the coss element.
  if (render) {
    return (
      <CossButton
        {...props}
        {...common}
        render={render}
        ref={ref}
        aria-busy={isPending || undefined}
        onClick={handleClick}
        loading={false}
        disabled={isDisabled}
        // Pending stays focusable (aria-disabled), so focus is not lost.
        aria-disabled={isPending || undefined}
        className={cn(fullWidth && "w-full", className)}
        {...marks}
      >
        {content}
      </CossButton>
    );
  }

  // react-aria triggers attach to their child through react-aria contexts, so
  // the button is a react-aria button until those triggers are gone.
  return (
    <AriaButton
      {...(props as React.ComponentProps<typeof AriaButton>)}
      excludeFromTabOrder={excludeFromTabOrder}
      preventFocusOnPress={preventFocusOnPress}
      ref={(node) => {
        inner.current = node;
        if (typeof ref === "function") ref(node);
        else if (ref) ref.current = node;
      }}
      slot={slot}
      onClick={handleClick}
      isDisabled={isDisabled}
      isPending={isPending}
      className={cn(buttonVariants(common), fullWidth && "w-full", className)}
      data-slot="button"
      {...marks}
    >
      {content}
    </AriaButton>
  );
}
