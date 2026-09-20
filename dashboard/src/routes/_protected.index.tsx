import { createFileRoute } from "@tanstack/react-router";
import { Console } from "@/pages/Console";

export const Route = createFileRoute("/_protected/")({
  component: Console,
});
