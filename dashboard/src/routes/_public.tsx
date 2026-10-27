import { createFileRoute } from "@tanstack/react-router";
import { PublicLayout } from "@/components/custom/PublicLayout";
import {
  AppRouteError,
  PublicRouteNotFound,
} from "@/components/custom/RouteFeedback";
import { RoutePending } from "@/components/custom/RoutePending";

export const Route = createFileRoute("/_public")({
  component: PublicLayout,
  pendingComponent: RoutePending,
  errorComponent: AppRouteError,
  notFoundComponent: PublicRouteNotFound,
});
