import { Outlet } from "@tanstack/react-router";
import { useEffect } from "react";
import { AppNotifications } from "@/components/custom/AppNotifications";
import { useInstanceBranding } from "@/components/custom/instance-branding";

export function AppLayout() {
  const { name } = useInstanceBranding();
  useEffect(() => {
    document.title = name;
  }, [name]);
  return (
    <>
      <Outlet />
      <AppNotifications />
    </>
  );
}
