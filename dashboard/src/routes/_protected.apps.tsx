import { createFileRoute } from "@tanstack/react-router";
import { ConnectedApps } from "@/pages/ConnectedApps";

export const Route = createFileRoute("/_protected/apps")({
  component: ConnectedApps,
});
