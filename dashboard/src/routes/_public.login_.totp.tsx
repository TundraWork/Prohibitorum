import { createFileRoute } from "@tanstack/react-router";
import { loginLoader } from "@/app/login-loader";
import { loginSearch } from "@/app/login-search";
import { TotpPage } from "@/pages/Login";

export const Route = createFileRoute("/_public/login_/totp")({
  validateSearch: loginSearch,
  loader: loginLoader,
  component: TotpPage,
});
