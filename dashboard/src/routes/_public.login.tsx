import { createFileRoute } from "@tanstack/react-router";
import { loginLoader } from "@/app/login-loader";
import { loginLoaderDeps, loginSearch } from "@/app/login-search";
import { PasswordPage } from "@/pages/Login";

export const Route = createFileRoute("/_public/login")({
  validateSearch: loginSearch,
  loaderDeps: loginLoaderDeps,
  loader: loginLoader,
  component: PasswordPage,
});
