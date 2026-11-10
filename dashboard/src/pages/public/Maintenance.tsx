import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { logoutMutationOptions } from "@/api/mutations";
import {
  clearSessionQueries,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { pickMaintenanceNotice } from "@/app/maintenance-notice";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { localeTags, resolveLocale } from "@/i18n";

/** How often the page asks whether maintenance is over. */
const recheckInterval = 180_000;

/**
 * Where everyone but an administrator waits while the instance is under
 * maintenance. The page keeps asking, and moves on by itself once maintenance
 * ends. An administrator gets in from here, whatever accounts the browser
 * already holds.
 */
export function MaintenancePage() {
  const { name } = useInstanceBranding();
  const { i18n } = useLingui();
  const { data: config, refetch } = useSuspenseQuery({
    ...publicConfigQueryOptions(),
    refetchInterval: recheckInterval,
  });
  const { data: session } = useSuspenseQuery(sessionQueryOptions());
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [retrying, setRetrying] = useState(false);
  const signOut = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      // Stay here.
      await clearSessionQueries(queryClient);
      queryClient.setQueryData(sessionQueryOptions().queryKey, null);
    },
  });

  const over = !config.maintenanceMode;
  useEffect(() => {
    if (over) void navigate({ to: "/" });
  }, [over, navigate]);

  return (
    <PublicStep
      title={<Trans id="maintenance.title">{name} is under maintenance</Trans>}
      description={
        <Trans id="maintenance.description">
          Sign-in and app access are paused during maintenance.
        </Trans>
      }
      actions={
        <PublicStep.Actions>
          <Button
            pending={retrying}
            onClick={async () => {
              setRetrying(true);
              try {
                await refetch();
              } finally {
                setRetrying(false);
              }
            }}
            size="lg"
          >
            <Trans id="maintenance.retry">Try again</Trans>
          </Button>
          <Button
            // `add` keeps the sign-in page open when the browser already
            // holds an account: the administrator's is added beside it.
            onClick={() =>
              navigate({
                to: "/login",
                search: { admin: "1", add: "1", u: undefined },
              })
            }
            variant="ghost"
            size="lg"
          >
            <Trans id="maintenance.admin_sign_in">Administrator sign-in</Trans>
          </Button>
          {session && (
            <Button
              pending={signOut.isPending}
              onClick={() => signOut.mutate()}
              variant="ghost"
              size="lg"
            >
              <Trans id="console.logout">Sign out</Trans>
            </Button>
          )}
        </PublicStep.Actions>
      }
    >
      {(() => {
        const notice = pickMaintenanceNotice(
          config,
          resolveLocale(i18n.locale),
        );
        return notice ? (
          <SurfaceAlert>
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title lang={localeTags[notice.locale]}>
                {notice.text}
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        ) : null;
      })()}
    </PublicStep>
  );
}
