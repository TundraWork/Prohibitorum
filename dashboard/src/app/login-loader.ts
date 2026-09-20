import { redirect } from "@tanstack/react-router";
import { parseReturnTo } from "@/api/auth";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import type { RouterContext } from "@/routes/__root";

export async function loginLoader({
  context: { queryClient },
  location,
  cause,
}: {
  context: RouterContext;
  location: { searchStr: string };
  cause: "preload" | "enter" | "stay";
}) {
  // A mounted sign-in page may be displaying newly issued recovery codes.
  if (cause === "stay") return;
  const [, , session] = await Promise.all([
    queryClient.ensureQueryData(publicConfigQueryOptions()),
    queryClient.ensureQueryData(authStatusQueryOptions()),
    queryClient.fetchQuery(sessionQueryOptions()),
  ]);
  let returnTo: string | undefined;
  try {
    returnTo = parseReturnTo(location.searchStr);
  } catch {
    return;
  }
  if (session !== null && returnTo === undefined) {
    throw redirect({ to: "/", replace: true });
  }
}
