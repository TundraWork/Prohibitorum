import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { AddressInput } from "@/components/custom/AddressInput";

function Harness({ onSubmit }: { onSubmit: () => void }) {
  const [value, setValue] = useState("");
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <AddressInput
        aria-label="Address"
        value={value}
        onValueChange={setValue}
      />
    </form>
  );
}

describe("AddressInput", () => {
  it("submits the form on Enter instead of taking a line break", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);
    const user = userEvent.setup();

    await user.type(
      screen.getByRole("textbox", { name: "Address" }),
      "https://a.example{Enter}",
    );

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("textbox", { name: "Address" })).toHaveValue(
      "https://a.example",
    );
  });

  it("drops line breaks from pasted text", async () => {
    render(<Harness onSubmit={() => undefined} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("textbox", { name: "Address" }));
    await user.paste("https://a.example/\nauthorize");

    expect(screen.getByRole("textbox", { name: "Address" })).toHaveValue(
      "https://a.example/authorize",
    );
  });
});
