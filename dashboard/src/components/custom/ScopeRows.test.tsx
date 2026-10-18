import { render, screen, within } from "@testing-library/react";
import { KeyRound, Tag } from "lucide-react";
import { expect, it } from "vitest";
import { ScopeRows } from "@/components/custom/ScopeRows";

it("draws one row per scope with its title and detail", () => {
  render(
    <ScopeRows
      rows={[
        {
          key: "a",
          icon: <Tag />,
          title: "Display name",
          detail: "Your display name",
        },
        { key: "b", icon: <KeyRound />, title: "wiki:write" },
      ]}
    />,
  );
  const items = within(screen.getByRole("list")).getAllByRole("listitem");
  expect(items.map((item) => item.textContent)).toEqual([
    "Display nameYour display name",
    "wiki:write",
  ]);
});

it("names the list after the label that introduces it", () => {
  render(
    <>
      <p id="heading">Wiki will receive</p>
      <ScopeRows
        labelledBy="heading"
        rows={[{ key: "a", icon: <Tag />, title: "Display name" }]}
      />
    </>,
  );
  expect(
    screen.getByRole("list", { name: "Wiki will receive" }),
  ).toBeInTheDocument();
});
