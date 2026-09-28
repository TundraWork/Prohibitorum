import { Trans } from "@lingui/react/macro";
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
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/** How often the page asks whether maintenance is over. */
const recheckInterval = 180_000;

/**
 * Where everyone but an administrator waits while the instance is under
 * maintenance. The page keeps asking, and moves on by itself once maintenance
 * ends. An administrator who is not signed in gets in from here.
 */
export function MaintenancePage() {
  const { name } = useInstanceBranding();
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
      // Stay here: without a session the way out becomes the administrators'
      // sign-in.
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
          Signing in and using apps are paused during maintenance.
        </Trans>
      }
      actions={
        <PublicStep.Actions>
          <Button
            fullWidth
            isPending={retrying}
            onPress={async () => {
              setRetrying(true);
              try {
                await refetch();
              } finally {
                setRetrying(false);
              }
            }}
          >
            <Trans id="maintenance.retry">Try again</Trans>
          </Button>
          {session ? (
            <Button
              variant="tertiary"
              fullWidth
              isPending={signOut.isPending}
              onPress={() => signOut.mutate()}
            >
              <Trans id="console.logout">Sign out</Trans>
            </Button>
          ) : (
            <Button
              variant="tertiary"
              fullWidth
              onPress={() =>
                navigate({ to: "/login", search: { admin: true } })
              }
            >
              <Trans id="maintenance.admin_sign_in">
                Administrator sign-in
              </Trans>
            </Button>
          )}
        </PublicStep.Actions>
      }
    >
      {config.maintenanceMessage && (
        <SurfaceAlert>
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{config.maintenanceMessage}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
    </PublicStep>
  );
}
