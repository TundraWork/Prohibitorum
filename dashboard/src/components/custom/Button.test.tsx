import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Button } from "@/components/custom/Button";

const spinner = () => document.querySelector('[data-slot="spinner"]');

describe("Button", () => {
  it("reports the default variant and size when unconfigured", () => {
    render(<Button>Save</Button>);
    const button = screen.getByRole("button");
    expect(button).toHaveAttribute("data-variant", "default");
    expect(button).toHaveAttribute("data-size", "default");
  });

  it("draws no spinner while idle", () => {
    render(<Button>Save</Button>);
    expect(spinner()).toBeNull();
    expect(screen.getByRole("button")).not.toHaveAttribute("aria-busy");
  });

  it("fires onClick on click, Enter and Space", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(<Button onClick={onClick}>Go</Button>);
    const button = screen.getByRole("button", { name: "Go" });
    await user.click(button);
    button.focus();
    await user.keyboard("{Enter}");
    await user.keyboard(" ");
    expect(onClick).toHaveBeenCalledTimes(3);
  });

  it("is natively disabled and ignores clicks when disabled", async () => {
    const onClick = vi.fn();
    render(
      <Button disabled onClick={onClick}>
        Go
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Go" });
    expect(button).toBeDisabled();
    await userEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("keeps the label beside the spinner while pending", () => {
    render(<Button pending>Save</Button>);
    const button = screen.getByRole("button", { name: "Save" });
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button).toHaveAttribute("aria-disabled", "true");
    expect(button).toBeEnabled();
    expect(spinner()).not.toBeNull();
    expect(button).toHaveTextContent("Save");
  });

  it("replaces the icon of an icon button with the spinner while pending", () => {
    render(
      <Button pending size="icon-sm" aria-label="Sign out">
        <svg data-testid="icon" />
      </Button>,
    );
    expect(spinner()).not.toBeNull();
    expect(screen.queryByTestId("icon")).toBeNull();
    expect(screen.getByRole("button", { name: "Sign out" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("shows the icon of an idle icon button", () => {
    render(
      <Button size="icon-sm" aria-label="Sign out">
        <svg data-testid="icon" />
      </Button>,
    );
    expect(spinner()).toBeNull();
    expect(screen.getByTestId("icon")).toBeTruthy();
  });

  it("ignores clicks while pending", async () => {
    const onClick = vi.fn();
    render(
      <Button pending onClick={onClick}>
        Go
      </Button>,
    );
    await userEvent.click(screen.getByRole("button"));
    expect(onClick).not.toHaveBeenCalled();
  });

  it("ignores Enter and Space while pending", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(
      <Button pending onClick={onClick}>
        Go
      </Button>,
    );
    screen.getByRole("button").focus();
    await user.keyboard("{Enter}");
    await user.keyboard(" ");
    expect(onClick).not.toHaveBeenCalled();
  });

  it("does not submit its form while pending", async () => {
    const onSubmit = vi.fn((event: React.FormEvent) => event.preventDefault());
    render(
      <form onSubmit={onSubmit}>
        <Button pending type="submit">
          Go
        </Button>
      </form>,
    );
    await userEvent.click(screen.getByRole("button"));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps keyboard focus when it becomes pending", () => {
    const { rerender } = render(<Button>Save</Button>);
    const button = screen.getByRole("button", { name: "Save" });
    button.focus();
    rerender(<Button pending>Save</Button>);
    expect(button).toHaveFocus();
  });

  it("renders the given element with the button's classes", () => {
    render(
      <Button render={<a href="/x" />} variant="outline">
        Open
      </Button>,
    );
    const link = screen.getByRole("link", { name: "Open" });
    expect(link).toHaveAttribute("href", "/x");
    expect(link).toHaveAttribute("data-slot", "button");
    expect(link.className).toContain("border-input");
  });

  it("marks a rendered element aria-disabled and ignores clicks while pending", async () => {
    const onClick = vi.fn();
    render(
      <Button render={<a href="/x" />} pending onClick={onClick}>
        Open
      </Button>,
    );
    const link = screen.getByRole("link", { name: "Open" });
    expect(link).toHaveAttribute("aria-disabled", "true");
    await userEvent.click(link);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("forwards a ref to the DOM button", () => {
    const ref = { current: null as HTMLButtonElement | null };
    render(<Button ref={ref}>Go</Button>);
    expect(ref.current).toBe(screen.getByRole("button"));
  });

  it("merges className and keeps the submit type", () => {
    render(
      <Button className="w-full" type="submit">
        Go
      </Button>,
    );
    const button = screen.getByRole("button");
    expect(button.className).toContain("w-full");
    expect(button).toHaveAttribute("type", "submit");
  });

  it("tabIndex -1 keeps the button out of the tab order", () => {
    render(<Button tabIndex={-1}>Go</Button>);
    expect(screen.getByRole("button", { name: "Go" })).toHaveAttribute(
      "tabindex",
      "-1",
    );
  });
});
