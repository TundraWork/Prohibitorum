import { createFileRoute } from "@tanstack/react-router";
import { loginLoader } from "@/app/login-loader";
import { PasswordPage } from "@/pages/Login";

export const Route = createFileRoute("/_public/login")({
  loader: loginLoader,
  component: PasswordPage,
});
