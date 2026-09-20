import { createFileRoute } from "@tanstack/react-router";
import { loginLoader } from "@/app/login-loader";
import { RecoveryPage } from "@/pages/Login";

export const Route = createFileRoute("/_public/login_/recovery")({
  loader: loginLoader,
  component: RecoveryPage,
});
