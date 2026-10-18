import type { ReactNode } from "react";

/**
 * The text of a tooltip for assistive technology. Base UI's tooltip sets
 * neither `aria-describedby` nor a role, so the trigger points at this element
 * (`aria-describedby={id}`) to carry the same text.
 */
export function SrDescription({
  id,
  children,
}: {
  id: string;
  children: ReactNode;
}) {
  return (
    <span id={id} className="sr-only">
      {children}
    </span>
  );
}
