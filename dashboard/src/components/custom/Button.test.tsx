import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Button } from "@/components/custom/Button";

const spinner = () => document.querySelector('[data-slot="spinner"]');

const variants = [
  "primary",
  "secondary",
  "tertiary",
  "ghost",
  "outline",
  "danger",
  "danger-soft",
  "warning",
] as const;

describe("Button", () => {
  it.each(variants)("maps the %s variant", (variant) => {
    render(<Button variant={variant}>Go</Button>);
    expect(screen.getByRole("button", { name: "Go" })).toHaveAttribute(
      "data-variant",
      variant,
    );
  });

  it("defaults to the primary variant and md size", () => {
    render(<Button>Go</Button>);
    const button = screen.getByRole("button", { name: "Go" });
    expect(button).toHaveAttribute("data-variant", "primary");
    expect(button).toHaveAttribute("data-size", "md");
  });

  it("draws no spinner while idle", () => {
    render(<Button>Save</Button>);
    expect(spinner()).toBeNull();
    expect(screen.getByRole("button")).not.toHaveAttribute("aria-busy");
  });

  it("keeps the label beside the spinner while pending", () => {
    render(<Button isPending>Save</Button>);
    const button = screen.getByRole("button", { name: /Save/ });
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button).toBeDisabled();
    expect(spinner()).not.toBeNull();
    expect(button).toHaveTextContent("Save");
  });

  it("replaces the icon of an icon-only button while pending", () => {
    render(
      <Button isPending isIconOnly aria-label="Sign out">
        <svg data-testid="icon" />
      </Button>,
    );
    expect(spinner()).not.toBeNull();
    expect(screen.queryByTestId("icon")).toBeNull();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeDisabled();
  });

  it("disables and does not fire onPress when isDisabled", async () => {
    const onPress = vi.fn();
    render(
      <Button isDisabled onPress={onPress}>
        Go
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Go" });
    expect(button).toBeDisabled();
    await userEvent.click(button);
    expect(onPress).not.toHaveBeenCalled();
  });

  it("does not fire onPress while pending", async () => {
    const onPress = vi.fn();
    render(
      <Button isPending onPress={onPress}>
        Go
      </Button>,
    );
    await userEvent.click(screen.getByRole("button"));
    expect(onPress).not.toHaveBeenCalled();
  });

  it("fires onPress on click, Enter and Space", async () => {
    const onPress = vi.fn();
    const user = userEvent.setup();
    render(<Button onPress={onPress}>Go</Button>);
    const button = screen.getByRole("button", { name: "Go" });
    await user.click(button);
    button.focus();
    await user.keyboard("{Enter}");
    await user.keyboard(" ");
    expect(onPress).toHaveBeenCalledTimes(3);
  });

  it("still fires a plain onClick", async () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Go</Button>);
    await userEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("renders the given element with the button's look", () => {
    render(
      <Button render={<a href="/x" />} variant="secondary">
        Open
      </Button>,
    );
    const link = screen.getByRole("link", { name: "Open" });
    expect(link).toHaveAttribute("href", "/x");
    expect(link).toHaveAttribute("data-variant", "secondary");
    expect(link.className).toContain("inline-flex");
  });

  it("forwards a ref to the DOM button", () => {
    const ref = { current: null as HTMLButtonElement | null };
    render(<Button ref={ref}>Go</Button>);
    expect(ref.current).toBe(screen.getByRole("button"));
  });

  it("fullWidth stretches the button and submit type is preserved", () => {
    render(
      <Button fullWidth type="submit">
        Go
      </Button>,
    );
    const button = screen.getByRole("button");
    expect(button.className).toContain("w-full");
    expect(button).toHaveAttribute("type", "submit");
  });
});
