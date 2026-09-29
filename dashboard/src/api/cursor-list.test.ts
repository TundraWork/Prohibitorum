import { hashKey } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { cursorListOptions, cursorPagesMarker } from "@/api/cursor-list";
import {
  forwardAuthAppQueryOptions,
  forwardAuthAppsListOptions,
  identityProviderQueryOptions,
  identityProvidersListOptions,
  identityProvidersQueryOptions,
  oidcAppQueryOptions,
  oidcAppsListOptions,
} from "@/api/queries";

/** The key `useCursorList` gives the infinite query over this prefix. */
function listKey(queryKey: readonly unknown[]) {
  return hashKey(
    cursorListOptions({
      queryKey,
      queryFn: async () => ({ items: [], nextCursor: "" }),
    }).queryKey,
  );
}

describe("cursorListOptions", () => {
  it("ends every list key in the cursor-pages marker", () => {
    const { queryKey } = cursorListOptions(oidcAppsListOptions());
    expect(queryKey).toEqual(["admin", "oidc-applications", cursorPagesMarker]);
  });

  it("keeps a record named `page` off its list's key", () => {
    expect(hashKey(oidcAppQueryOptions("page").queryKey)).not.toBe(
      listKey(oidcAppsListOptions().queryKey),
    );
    expect(hashKey(forwardAuthAppQueryOptions("page").queryKey)).not.toBe(
      listKey(forwardAuthAppsListOptions().queryKey),
    );
    expect(hashKey(identityProviderQueryOptions("page").queryKey)).not.toBe(
      listKey(identityProvidersListOptions().queryKey),
    );
  });

  it("keeps a plain read of the same prefix off the list's key", () => {
    expect(hashKey(identityProvidersQueryOptions().queryKey)).not.toBe(
      listKey(identityProvidersListOptions().queryKey),
    );
  });
});
