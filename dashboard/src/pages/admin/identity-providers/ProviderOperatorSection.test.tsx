import { I18nProvider } from "@lingui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { ProviderOperatorSection } from "@/pages/admin/identity-providers/ProviderOperatorSection";

vi.mock("@/api/mutations", () => ({
  startOperatorSessionMutationOptions: () => ({}),
  validateOperatorSessionMutationOptions: () => ({}),
  verifyOperatorSessionMutationOptions: () => ({}),
}));

beforeEach(() => {
  i18n.activate("en");
});

/** The provider an unconfigured VRChat operator session describes. */
const provider = {
  slug: "vrchat",
  displayName: "VRChat",
  secretStatus: "unconfigured",
  secretValidatedAt: null,
};

function mount(): UserEvent {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient()}>
        <ProviderOperatorSection provider={provider} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return userEvent.setup();
}

/** The opened sign-in dialog. */
async function openDialog(user: UserEvent): Promise<HTMLElement> {
  await user.click(await screen.findByRole("button", { name: "Sign in" }));
  return screen.findByRole("dialog");
}

describe("provider operator sign-in dialog", () => {
  it("keeps the dialog open on a backdrop click and Escape", async () => {
    const user = mount();
    const dialog = await openDialog(user);

    const backdrop = document.querySelector('[data-slot="modal-backdrop"]');
    if (backdrop === null) throw new Error("no modal backdrop");
    await user.click(backdrop);
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    // The backdrop click leaves focus outside the dialog, where Escape does
    // nothing; a focused field is where a reader's stray Escape actually lands.
    await user.click(within(dialog).getByRole("textbox", { name: "Username" }));
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
  });

  it("closes from its own Cancel button", async () => {
    const user = mount();
    const dialog = await openDialog(user);

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });
});
