import {
  createFileRoute,
  lazyRouteComponent,
  notFound,
} from "@tanstack/react-router";
import { PublicPending } from "@/components/custom/RouteFeedback";

export const Route = createFileRoute("/_public/_preview/__dev/forms")({
  beforeLoad: () => {
    if (!import.meta.env.DEV) throw notFound();
  },
  pendingComponent: PublicPending,
  component: lazyRouteComponent(() => import("@/pages/DevForms")),
});
