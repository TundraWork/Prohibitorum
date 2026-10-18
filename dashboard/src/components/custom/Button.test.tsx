import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Dialog,
  DialogTrigger,
  Menu,
  MenuItem,
  MenuTrigger,
  Popover,
} from "react-aria-components";
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
    const button = screen.getByRole("button", { name: "Save" });
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button).toHaveAttribute("aria-disabled", "true");
    expect(button).toBeEnabled();
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
    expect(screen.getByRole("button", { name: "Sign out" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
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

  it("keeps keyboard focus when it becomes pending", () => {
    const { rerender } = render(<Button>Save</Button>);
    const button = screen.getByRole("button", { name: "Save" });
    button.focus();
    rerender(<Button isPending>Save</Button>);
    expect(button).toHaveFocus();
  });

  it("does not fire onPress on Enter while pending", async () => {
    const onPress = vi.fn();
    const user = userEvent.setup();
    render(
      <Button isPending onPress={onPress}>
        Go
      </Button>,
    );
    screen.getByRole("button").focus();
    await user.keyboard("{Enter}");
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
    expect(link).toHaveAttribute("data-slot", "button");
  });

  it("marks a rendered element aria-disabled and ignores clicks while pending", async () => {
    const onClick = vi.fn();
    render(
      <Button render={<a href="/x" />} isPending onClick={onClick}>
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

  it("fires onPress and onClick once each for click, Enter and Space", async () => {
    const onPress = vi.fn();
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(
      <Button onPress={onPress} onClick={onClick}>
        Go
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Go" });
    await user.click(button);
    expect(onPress).toHaveBeenCalledTimes(1);
    expect(onClick).toHaveBeenCalledTimes(1);
    button.focus();
    await user.keyboard("{Enter}");
    expect(onPress).toHaveBeenCalledTimes(2);
    expect(onClick).toHaveBeenCalledTimes(2);
    await user.keyboard(" ");
    expect(onPress).toHaveBeenCalledTimes(3);
    expect(onClick).toHaveBeenCalledTimes(3);
  });

  describe("inside react-aria triggers", () => {
    const menu = (button: React.ReactElement) => (
      <MenuTrigger>
        {button}
        <Popover>
          <Menu>
            <MenuItem id="a">Alpha</MenuItem>
          </Menu>
        </Popover>
      </MenuTrigger>
    );

    it("opens a menu on click and marks the trigger expanded", async () => {
      render(menu(<Button>Menu</Button>));
      const trigger = screen.getByRole("button", { name: "Menu" });
      expect(trigger).toHaveAttribute("aria-expanded", "false");
      await userEvent.click(trigger);
      expect(
        await screen.findByRole("menuitem", { name: "Alpha" }),
      ).toBeTruthy();
      expect(trigger).toHaveAttribute("aria-expanded", "true");
    });

    it("opens a menu with ArrowDown on the focused trigger", async () => {
      const user = userEvent.setup();
      render(menu(<Button>Menu</Button>));
      const trigger = screen.getByRole("button", { name: "Menu" });
      trigger.focus();
      await user.keyboard("{ArrowDown}");
      expect(
        await screen.findByRole("menuitem", { name: "Alpha" }),
      ).toBeTruthy();
      expect(trigger).toHaveAttribute("aria-expanded", "true");
    });

    it("opens a popover and marks the trigger expanded", async () => {
      render(
        <DialogTrigger>
          <Button>Info</Button>
          <Popover>
            <Dialog aria-label="Details">Body</Dialog>
          </Popover>
        </DialogTrigger>,
      );
      const trigger = screen.getByRole("button", { name: "Info" });
      await userEvent.click(trigger);
      expect(
        await screen.findByRole("dialog", { name: "Details" }),
      ).toBeTruthy();
      expect(trigger).toHaveAttribute("aria-expanded", "true");
    });

    it.each([
      ["disabled", { isDisabled: true }],
      ["pending", { isPending: true }],
    ])("does not open a menu when %s", async (_name, props) => {
      render(menu(<Button {...props}>Menu</Button>));
      await userEvent.click(screen.getByRole("button", { name: "Menu" }));
      expect(screen.queryByRole("menuitem")).toBeNull();
    });
  });

  it("excludeFromTabOrder renders tabindex -1", () => {
    render(<Button excludeFromTabOrder>Go</Button>);
    expect(screen.getByRole("button", { name: "Go" })).toHaveAttribute(
      "tabindex",
      "-1",
    );
  });

  it("preventFocusOnPress keeps focus where it was", async () => {
    const user = userEvent.setup();
    render(
      <>
        <input aria-label="field" />
        <Button preventFocusOnPress>Go</Button>
      </>,
    );
    const field = screen.getByLabelText("field");
    field.focus();
    await user.click(screen.getByRole("button", { name: "Go" }));
    expect(document.activeElement).toBe(field);
  });
});
