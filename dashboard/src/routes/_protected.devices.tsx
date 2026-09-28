import { createFileRoute } from "@tanstack/react-router";
import { DevicesPage } from "@/pages/Devices";
import { devicesSearch } from "@/pages/devices/pairing-code";

export const Route = createFileRoute("/_protected/devices")({
  validateSearch: devicesSearch,
  component: DevicesPage,
});
