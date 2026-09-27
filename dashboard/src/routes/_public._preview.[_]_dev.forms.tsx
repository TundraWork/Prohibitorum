import {
  createFileRoute,
  lazyRouteComponent,
  notFound,
  rootRouteId,
} from "@tanstack/react-router";
import { PublicPending } from "@/components/custom/RouteFeedback";

export const Route = createFileRoute("/_public/_preview/__dev/forms")({
  beforeLoad: () => {
    // Hidden in production as if it did not exist: the whole-window not-found
    // view, not the sign-in card's.
    if (!import.meta.env.DEV) throw notFound({ routeId: rootRouteId });
  },
  pendingComponent: PublicPending,
  component: lazyRouteComponent(() => import("@/pages/DevForms")),
});
