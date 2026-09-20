import { createFileRoute } from "@tanstack/react-router";
import { loginLoader } from "@/app/login-loader";
import { TotpPage } from "@/pages/Login";

export const Route = createFileRoute("/_public/login_/totp")({
  loader: loginLoader,
  component: TotpPage,
});
