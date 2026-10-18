import { type screen, within } from "@testing-library/react";

type Scope = Pick<typeof screen, "getAllByRole" | "findAllByRole">;

/**
 * A one-time-code field draws one textbox per character, all named by the
 * field's label; these helpers read them as the one field a person sees.
 */
export function otpSlots(name: string, scope: Scope = within(document.body)) {
  return scope.getAllByRole("textbox", { name });
}

/** The slot a person focuses first, and the one that carries the field's state. */
export function firstOtpSlot(name: string, scope?: Scope): HTMLElement {
  const [first] = otpSlots(name, scope);
  if (!first) throw new Error(`No code field named "${name}"`);
  return first;
}

/** Waits for the field to appear, then returns its first slot. */
export async function findFirstOtpSlot(
  name: string,
  scope: Scope = within(document.body),
): Promise<HTMLElement> {
  const [first] = await scope.findAllByRole("textbox", { name });
  if (!first) throw new Error(`No code field named "${name}"`);
  return first;
}

/** What the slots hold together. */
export function otpValue(name: string, scope?: Scope): string {
  return otpSlots(name, scope)
    .map((slot) => (slot as HTMLInputElement).value)
    .join("");
}
