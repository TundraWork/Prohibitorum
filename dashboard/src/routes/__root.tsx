import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext } from "@tanstack/react-router";
import { publicConfigQueryOptions } from "@/api/queries";
import { AppLayout } from "@/components/custom/AppLayout";

export type RouterContext = { queryClient: QueryClient };

export const Route = createRootRouteWithContext<RouterContext>()({
  // The instance's name, icon and background are drawn by every layout and by
  // the document title, so they are in hand before anything paints.
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(publicConfigQueryOptions()),
  component: AppLayout,
});
