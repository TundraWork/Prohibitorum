import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { WrenchIcon } from "lucide-react";
import { publicConfigQueryOptions } from "@/api/queries";

/**
 * A quiet bar above every console page while maintenance mode is on, so an
 * administrator who is still signed in never forgets that everyone else is
 * locked out. It reads `/config`, the same query the setting writes through,
 * and draws nothing while the mode is off.
 */
export function MaintenanceBanner(): React.ReactElement | null {
  const { t } = useLingui();
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  if (!config.maintenanceMode) return null;

  return (
    <div
      role="status"
      aria-label={t({
        id: "console.maintenance.label",
        message: "Maintenance mode",
      })}
      className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b bg-warning/8 px-4 py-2 text-sm"
    >
      <WrenchIcon
        aria-hidden="true"
        className="size-4 shrink-0 text-warning-foreground"
      />
      <p className="min-w-0 text-pretty text-warning-foreground">
        <Trans id="console.maintenance.banner">
          Maintenance mode is on — only administrators can sign in.
        </Trans>
      </p>
      <Link
        to="/admin/settings"
        hash="maintenance"
        className="rounded-sm font-medium underline underline-offset-4 outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <Trans id="console.maintenance.link">Maintenance settings</Trans>
      </Link>
    </div>
  );
}
