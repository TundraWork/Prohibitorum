import { describe, expect, it } from "vitest";
import type { AppGroupView } from "@/api/raw-admin-paths";
import {
  groupUpdateBody,
  validateGroupName,
  validateGroupSlug,
} from "@/pages/admin/groups/group-validation";

describe("validateGroupName", () => {
  it("asks for a name when it is empty or blank", () => {
    expect(validateGroupName("")?.id).toBe("admin.group.display-name.required");
    expect(validateGroupName("   ")?.id).toBe(
      "admin.group.display-name.required",
    );
  });

  it("accepts any text with something in it", () => {
    expect(validateGroupName("Staff")).toBeUndefined();
  });
});

describe("validateGroupSlug", () => {
  it("asks for an identifier when it is empty or blank", () => {
    expect(validateGroupSlug("")?.id).toBe("admin.group.slug.required");
    expect(validateGroupSlug("  ")?.id).toBe("admin.group.slug.required");
  });

  it.each(["staff", "a", "passkey-users", "team-2", "a".repeat(64)])(
    "accepts %s",
    (slug) => {
      expect(validateGroupSlug(slug)).toBeUndefined();
    },
  );

  it.each([
    "Staff",
    "-staff",
    "staff-",
    "two--hyphens",
    "has space",
    "under_score",
    "a".repeat(65),
  ])("states the shape for %s", (slug) => {
    expect(validateGroupSlug(slug)?.id).toBe("admin.group.slug.format");
  });
});

describe("groupUpdateBody", () => {
  const group: AppGroupView = {
    id: 7,
    kind: "rule",
    slug: "passkey-users",
    displayName: "Passkey users",
    exposedToDownstream: true,
    applicationCount: 2,
  };

  it("carries every stored field, with an omitted description as empty", () => {
    expect(groupUpdateBody(group, {})).toEqual({
      slug: "passkey-users",
      displayName: "Passkey users",
      description: "",
      exposedToDownstream: true,
    });
  });

  it("lets a section replace only the fields it edits", () => {
    expect(
      groupUpdateBody(
        { ...group, description: "Who signs in with one" },
        { exposedToDownstream: false },
      ),
    ).toEqual({
      slug: "passkey-users",
      displayName: "Passkey users",
      description: "Who signs in with one",
      exposedToDownstream: false,
    });
  });
});
