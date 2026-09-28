import { createFileRoute } from "@tanstack/react-router";
import { loginLoader } from "@/app/login-loader";
import { loginSearch } from "@/app/login-search";
import { RecoveryPage } from "@/pages/Login";

export const Route = createFileRoute("/_public/login_/recovery")({
  validateSearch: loginSearch,
  loader: loginLoader,
  component: RecoveryPage,
});
