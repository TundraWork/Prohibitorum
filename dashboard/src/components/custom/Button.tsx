import type * as React from "react";
import { Button as CossButton } from "@/components/ui/button";
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
  extends Omit<React.ComponentProps<"button">, "onClick" | "disabled"> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  isIconOnly?: boolean;
  isPending?: boolean;
  isDisabled?: boolean;
  fullWidth?: boolean;
  onPress?: (event: React.MouseEvent<HTMLButtonElement>) => void;
  onClick?: (event: React.MouseEvent<HTMLButtonElement>) => void;
  render?: React.ReactElement;
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
  className,
  children,
  ...props
}: ButtonProps): React.ReactElement {
  return (
    <CossButton
      {...props}
      variant={cossVariant[variant]}
      size={cossSize[size][isIconOnly ? "icon" : "text"]}
      loading={false}
      disabled={isDisabled || isPending}
      aria-busy={isPending || undefined}
      data-variant={variant}
      data-size={size}
      className={cn(fullWidth && "w-full", className)}
      onClick={(event: React.MouseEvent<HTMLButtonElement>) => {
        onClick?.(event);
        onPress?.(event);
      }}
    >
      {isPending && <Spinner aria-hidden="true" data-slot="spinner" />}
      {isPending && isIconOnly ? null : children}
    </CossButton>
  );
}
