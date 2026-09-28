import { redirect } from "@tanstack/react-router";
import { readReturnTo } from "@/api/auth";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import type { loginLoaderDeps } from "@/app/login-search";
import type { RouterContext } from "@/routes/__root";

export async function loginLoader({
  context: { queryClient },
  deps,
  cause,
}: {
  context: RouterContext;
  deps: ReturnType<typeof loginLoaderDeps>;
  cause: "preload" | "enter" | "stay";
}) {
  // A mounted sign-in page may be displaying newly issued recovery codes.
  if (cause === "stay") return;
  const [, , session] = await Promise.all([
    queryClient.query(publicConfigQueryOptions()),
    queryClient.query(authStatusQueryOptions()),
    queryClient.query(sessionQueryOptions()),
  ]);
  let returnTo: string | undefined;
  try {
    returnTo = readReturnTo(deps.returnTo);
  } catch {
    return;
  }
  if (session !== null && returnTo === undefined) {
    throw redirect({ to: "/", replace: true });
  }
}
