import { createFileRoute } from "@tanstack/react-router";
import { loginAppearanceQueryOptions } from "@/api/queries";
import { SignInPageEditor } from "@/pages/admin/settings/sign-in-page/SignInPageEditor";

export const Route = createFileRoute(
  "/_protected/admin/_admin/settings_/sign-in-page",
)({
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(loginAppearanceQueryOptions()),
  component: SignInPageEditor,
});
