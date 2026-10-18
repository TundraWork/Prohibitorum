import { type screen, within } from "@testing-library/react";

type Scope = Pick<typeof screen, "getAllByRole" | "findAllByRole">;

/**
 * A one-time-code field draws one textbox per character; the first is named by
 * the field's label and the rest by position; these helpers read them as the
 * one field a person sees.
 */
export function otpSlots(name: string, scope: Scope = within(document.body)) {
  return slotsOf(firstOtpSlot(name, scope));
}

/** Every textbox in the first slot's group, in order. */
function slotsOf(first: HTMLElement): HTMLElement[] {
  return within(first.parentElement as HTMLElement).getAllByRole("textbox");
}

/** The slot a person focuses first, and the one that carries the field's state. */
export function firstOtpSlot(
  name: string,
  scope: Scope = within(document.body),
): HTMLElement {
  const [first] = scope.getAllByRole("textbox", { name });
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
