import { createFileRoute } from "@tanstack/react-router";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import { ApiPreview } from "@/pages/ApiPreview";

export const Route = createFileRoute("/_public/_preview/preview/api")({
  loader: async ({ context: { queryClient } }) => {
    await Promise.all([
      queryClient.ensureQueryData(publicConfigQueryOptions()),
      queryClient.ensureQueryData(authStatusQueryOptions()),
    ]);
  },
  component: ApiPreview,
});
