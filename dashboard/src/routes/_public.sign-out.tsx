import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { optionalSearchText } from "@/app/search-params";
import { SignOutPage } from "@/pages/public/SignOut";

/**
 * Where sso-logout sends a sign-out it cannot tell the reader asked for: a
 * forward-auth application's sign-out without a valid pass. `rd` is the
 * address to return to, which only the server checks; the page reads and
 * writes nothing until the reader answers.
 */
export const Route = createFileRoute("/_public/sign-out")({
  validateSearch: z.object({ rd: optionalSearchText() }),
  component: SignOutPage,
});
