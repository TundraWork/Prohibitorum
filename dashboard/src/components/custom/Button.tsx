import { buttonVariants, Button as HeroUIButton, Spinner } from "@heroui/react";
import type { ComponentProps } from "react";
import { tv } from "tailwind-variants";

type HeroUIButtonProps = ComponentProps<typeof HeroUIButton>;

type ButtonProps = Omit<HeroUIButtonProps, "variant"> & {
  variant?: HeroUIButtonProps["variant"] | "warning";
};

/**
 * HeroUI's variants plus `warning`, added the way HeroUI's "Adding custom
 * variants" example does: `tv` extending `buttonVariants`.
 *
 * The library has no warning button, and the console needs one for a save that
 * carries a consequence (see `SubmitButton`'s `tone`). It sets the same four
 * tokens `.button--danger` sets, from the theme's warning colours, so hover and
 * press are derived the way the library derives them for every other variant.
 */
const appButtonVariants = tv({
  extend: buttonVariants,
  variants: {
    variant: {
      warning:
        "[--button-bg:var(--warning)] [--button-bg-hover:var(--warning-hover)] [--button-bg-pressed:var(--warning-hover)] [--button-fg:var(--warning-foreground)]",
    },
  },
});

/**
 * HeroUI's button with a pending state that always shows progress: while
 * `isPending` is true the button draws the spinner itself, so screens do not
 * draw one of their own.
 *
 * A button with text keeps that text beside the spinner. An icon-only button
 * has room for one glyph, so the spinner takes the icon's place. The spinner is
 * decorative: HeroUI's own spinner carries an English `Loading` label, which
 * would otherwise join the button's accessible name. Callers that want
 * different wording while pending still receive HeroUI's render props
 * unchanged.
 */
export function Button({
  children,
  isIconOnly,
  variant,
  className,
  size,
  fullWidth,
  ...props
}: ButtonProps) {
  // The warning variant is ours, so HeroUI draws its default and the extended
  // classes override the colour tokens on top of it.
  const warningClass =
    variant === "warning"
      ? (extra?: string) =>
          appButtonVariants({
            variant: "warning",
            size,
            fullWidth,
            isIconOnly,
            className: extra,
          })
      : undefined;

  return (
    <HeroUIButton
      isIconOnly={isIconOnly}
      size={size}
      fullWidth={fullWidth}
      variant={variant === "warning" ? undefined : variant}
      className={
        warningClass === undefined
          ? className
          : typeof className === "function"
            ? (values) => warningClass(className(values))
            : warningClass(className)
      }
      {...props}
    >
      {(values) => {
        const content =
          values.isPending && isIconOnly
            ? null
            : typeof children === "function"
              ? children(values)
              : children;
        return (
          <>
            {values.isPending && (
              <Spinner size="sm" color="current" aria-hidden="true" />
            )}
            {content}
          </>
        );
      }}
    </HeroUIButton>
  );
}
