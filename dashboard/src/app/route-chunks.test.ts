import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";
import { publicPaths } from "../../tools/route-chunks";

const src = join(dirname(fileURLToPath(import.meta.url)), "..");
const routes = join(src, "routes");
const tree = readFileSync(join(src, "routeTree.gen.ts"), "utf8");

it("reads every public route's path from the generated route tree", () => {
  const paths = publicPaths(tree);
  expect(paths).toEqual(
    expect.arrayContaining([
      "/login",
      "/login/totp",
      "/consent",
      "/saml-consent",
      "/enroll/$token",
      "/require-identity/$ticket",
    ]),
  );
  // One path per page under the `_public` layout, and nothing from the console.
  const pages = readdirSync(routes).filter(
    (file) =>
      file.startsWith("_public.") &&
      file.endsWith(".tsx") &&
      file !== "_public.tsx",
  );
  expect(paths).toHaveLength(pages.length);
  expect(paths.some((path) => path.startsWith("/admin"))).toBe(false);
  expect(paths).not.toContain("/");
});
