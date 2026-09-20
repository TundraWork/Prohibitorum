import { createFileRoute } from "@tanstack/react-router";
import { Preview } from "@/pages/Preview";

export const Route = createFileRoute("/_public/_preview/preview/components")({
  component: Preview,
});
