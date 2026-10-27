import { useAtomValue } from "jotai";
import type * as React from "react";
import { use } from "react";
import { sudoDialogAtom } from "@/api/sudo";
import { SudoScopeContext } from "@/components/custom/sudo-scope";
import { Button as CossButton } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

type CossProps = React.ComponentProps<typeof CossButton>;

export type ButtonVariant = NonNullable<CossProps["variant"]>;
export type ButtonSize = NonNullable<CossProps["size"]>;

export interface ButtonProps extends React.ComponentProps<"button"> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  pending?: boolean;
  render?: React.ReactElement;
}

/**
 * coss's button with a `pending` state that keeps the button focusable.
 *
 * A pending button is `aria-disabled`, not natively disabled, so keyboard
 * focus survives the wait; its presses are ignored. A text button keeps its
 * label beside the spinner, while an icon button has room for one glyph and
 * shows the spinner in its place.
 *
 * One write shows one spinner. While the identity check is open it carries
 * that spinner, so a pending button behind it stays locked but does not spin.
 */
export function Button({
  pending = false,
  variant = "default",
  size = "default",
  onClick,
  children,
  ...props
}: ButtonProps): React.ReactElement {
  const iconOnly = size?.startsWith("icon") ?? false;
  const sudoOpen = useAtomValue(sudoDialogAtom) !== null;
  const inSudo = use(SudoScopeContext);
  const spinning = pending && (!sudoOpen || inSudo);
  return (
    <CossButton
      {...props}
      variant={variant}
      size={size}
      data-variant={variant}
      data-size={size}
      aria-busy={pending || undefined}
      aria-disabled={pending || props["aria-disabled"]}
      onClick={(event) => {
        if (pending) {
          event.preventDefault();
          return;
        }
        onClick?.(event);
      }}
    >
      {spinning && <Spinner aria-hidden="true" data-slot="spinner" />}
      {spinning && iconOnly ? null : children}
    </CossButton>
  );
}
