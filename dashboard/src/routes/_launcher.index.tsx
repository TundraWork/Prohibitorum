import { createFileRoute } from "@tanstack/react-router";
import { Launcher } from "@/pages/launcher/Launcher";

export const Route = createFileRoute("/_launcher/")({
  component: Launcher,
});
