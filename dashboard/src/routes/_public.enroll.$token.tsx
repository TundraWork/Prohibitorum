import { createFileRoute } from "@tanstack/react-router";
import {
  enrollmentQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import { EnrollPage } from "@/pages/public/Enroll";

/**
 * An enrollment link: an invitation, a reset an administrator sent, the
 * first administrator's link, or an account a VRChat verification is about
 * to create. A used or expired link fails to load, and the page says so.
 */
export const Route = createFileRoute("/_public/enroll/$token")({
  loader: ({ context: { queryClient }, params: { token } }) =>
    Promise.all([
      queryClient.ensureQueryData(enrollmentQueryOptions(token)),
      queryClient.ensureQueryData(publicConfigQueryOptions()),
    ]),
  component: EnrollPage,
});
