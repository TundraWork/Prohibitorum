import { Button as HeroUIButton, Spinner } from "@heroui/react";
import type { ComponentProps } from "react";

type ButtonProps = ComponentProps<typeof HeroUIButton>;

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
export function Button({ children, isIconOnly, ...props }: ButtonProps) {
  return (
    <HeroUIButton isIconOnly={isIconOnly} {...props}>
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
