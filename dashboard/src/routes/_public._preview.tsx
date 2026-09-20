import { createFileRoute } from "@tanstack/react-router";
import { PreviewLayout } from "@/components/custom/PreviewLayout";

export const Route = createFileRoute("/_public/_preview")({
  component: PreviewLayout,
});
