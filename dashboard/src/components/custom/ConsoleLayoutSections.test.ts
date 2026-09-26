import { describe, expect, it } from "vitest";
import { visibleManagementSections } from "@/components/custom/ConsoleLayout";

/**
 * What the sidebar shows, and therefore what the two gates let the account
 * reach.
 *
 * The rule is the same one `_protected.admin` and `_protected.admin._admin`
 * enforce on the routes, so these cases are the two of them read from the
 * outside: an entry that appears is a page the account can open, and an entry
 * that is missing is a page the loader turns away. They are written as the
 * combinations a real account has, rather than as a matrix of the three booleans,
 * because the combinations are what the sidebar has to get right.
 */
describe("management sections", () => {
  const paths = (sections: ReturnType<typeof visibleManagementSections>) =>
    sections.map((section) => section.path);

  it("shows an administrator everything", () => {
    expect(paths(visibleManagementSections(true, undefined))).toEqual([
      "/admin/users",
      "/admin/groups",
      "/admin/invitations",
      "/admin/identity-providers",
      "/admin/oidc-applications",
      "/admin/saml-applications",
      "/admin/forward-auth-apps",
      "/admin/logs",
      "/admin/settings",
    ]);
  });

  it("shows an unassigned member nothing at all", () => {
    // Not "the application sections": a member with no assignment has no
    // business in the management area, and the sidebar group is left out whole.
    expect(
      paths(
        visibleManagementSections(false, {
          oidc: false,
          saml: false,
          forwardAuth: false,
        }),
      ),
    ).toEqual([]);
  });

  it("shows the administrator-only pages to nobody but an administrator", () => {
    // Even fully assigned, a member never sees users, groups, invitations,
    // federation, logs or settings — the inner gate would redirect them.
    const sections = paths(
      visibleManagementSections(false, {
        oidc: true,
        saml: true,
        forwardAuth: true,
      }),
    );
    expect(sections).toEqual([
      "/admin/oidc-applications",
      "/admin/saml-applications",
      "/admin/forward-auth-apps",
    ]);
    expect(sections).not.toContain("/admin/users");
    expect(sections).not.toContain("/admin/identity-providers");
    expect(sections).not.toContain("/admin/settings");
  });

  it("shows one kind at a time, in the sidebar's order", () => {
    // A manager of SAML applications alone reaches SAML and nothing else; the
    // order stays the sidebar's, so the entry does not move depending on which
    // kind the account happens to manage.
    expect(
      paths(
        visibleManagementSections(false, {
          oidc: false,
          saml: true,
          forwardAuth: false,
        }),
      ),
    ).toEqual(["/admin/saml-applications"]);
    expect(
      paths(
        visibleManagementSections(false, {
          oidc: true,
          saml: false,
          forwardAuth: false,
        }),
      ),
    ).toEqual(["/admin/oidc-applications"]);
    expect(
      paths(
        visibleManagementSections(false, {
          oidc: false,
          saml: false,
          forwardAuth: true,
        }),
      ),
    ).toEqual(["/admin/forward-auth-apps"]);
  });

  it("shows nothing while the assignment read is still in flight", () => {
    // The entry appears once the answer arrives. Showing the whole group first
    // and removing it would offer links the loader then turns away, and the
    // first paint is the one a member sees most often.
    expect(paths(visibleManagementSections(false, undefined))).toEqual([]);
  });
});
