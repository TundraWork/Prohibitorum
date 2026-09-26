import { createFileRoute } from "@tanstack/react-router";
import { AdminSamlApplications } from "@/pages/admin/saml-applications/AdminSamlApplications";

/**
 * The SAML applications list, at the top level of the management area rather
 * than under `_admin`.
 *
 * A delegated manager reaches the applications assigned to them, so this route
 * hangs off `_protected.admin` — whose loader already admits "admin, or manages
 * at least one application" — and decides for itself whether this account
 * manages a SAML one. Everything admin-only is one level in, under `_admin`.
 *
 * The list endpoint filters to the caller's assignments, so the page makes no
 * distinction: whatever it receives is what the account may see.
 */
export const Route = createFileRoute("/_protected/admin/saml-applications/")({
  component: AdminSamlApplications,
});
