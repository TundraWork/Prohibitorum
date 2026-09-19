import {
  Avatar,
  Button,
  Drawer,
  Link,
  Spinner,
  useOverlayState,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createLink,
  Outlet,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import type { components } from "@/api/generated/schema";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import { instanceName } from "@/components/custom/AppLayout";
import { notificationQueue } from "@/components/custom/AppNotifications";
import { LanguageSelect } from "@/components/custom/LanguageSelect";
import { RouteError, RoutePending } from "@/components/custom/RouteFeedback";
import { ThemeToggle } from "@/components/custom/ThemeToggle";

type Session = components["schemas"]["SessionView"];
const NavigationLink = createLink(Link);

function ConsoleNavigation({ session }: { session: Session }) {
  const { t } = useLingui();
  return (
    <div className="flex min-w-0 flex-col gap-8">
      <div className="flex min-w-0 items-start gap-3">
        <Avatar className="shrink-0">
          {session.avatarUrl && <Avatar.Image src={session.avatarUrl} alt="" />}
          <Avatar.Fallback />
        </Avatar>
        <div className="min-w-0 wrap-anywhere">
          <p className="font-semibold">{session.displayName}</p>
          <p className="text-sm text-muted">{session.username}</p>
        </div>
      </div>
      <nav
        aria-label={t({
          id: "console.navigation",
          message: "Console navigation",
        })}
        className="flex flex-col items-start gap-5"
      >
        <NavigationLink to="/" activeOptions={{ exact: true }} preload={false}>
          <Trans id="console.home">Console home</Trans>
        </NavigationLink>
        <p className="text-sm text-muted">
          <Trans id="console.coming-soon">Coming later</Trans>
        </p>
        <Link isDisabled>
          <Trans id="console.profile">Profile</Trans>
        </Link>
        <Link isDisabled>
          <Trans id="console.security">Security</Trans>
        </Link>
        <Link isDisabled>
          <Trans id="console.applications">Applications</Trans>
        </Link>
        <Link isDisabled>
          <Trans id="console.devices">Devices</Trans>
        </Link>
      </nav>
    </div>
  );
}

function ConsoleActions({
  pending,
  onLogout,
}: {
  pending: boolean;
  onLogout: () => void;
}) {
  return (
    <div className="flex w-full flex-col items-start gap-3">
      <LanguageSelect />
      <ThemeToggle />
      <Button variant="outline" isPending={pending} onPress={onLogout}>
        {pending && <Spinner size="sm" />}
        <Trans id="console.logout">Sign out</Trans>
      </Button>
    </div>
  );
}

function ConsoleShell({
  session,
  pending,
  onLogout,
}: {
  session: Session;
  pending: boolean;
  onLogout: () => void;
}) {
  const { t } = useLingui();
  const drawer = useOverlayState();
  const router = useRouter();
  const { close } = drawer;
  useEffect(() => router.subscribe("onBeforeNavigate", close), [router, close]);
  useEffect(() => {
    const desktop = window.matchMedia("(min-width: 768px)");
    const onChange = () => {
      if (desktop.matches) close();
    };
    onChange();
    desktop.addEventListener("change", onChange);
    return () => desktop.removeEventListener("change", onChange);
  }, [close]);
  const logout = () => {
    close();
    onLogout();
  };
  return (
    <div className="min-h-dvh md:grid md:grid-cols-[17rem_minmax(0,1fr)]">
      <aside className="sticky top-0 hidden h-dvh min-w-0 flex-col gap-8 overflow-y-auto p-6 md:flex">
        <ConsoleNavigation session={session} />
        <div className="mt-auto pt-8">
          <ConsoleActions pending={pending} onLogout={logout} />
        </div>
      </aside>
      <div className="min-w-0">
        <header className="flex min-w-0 items-center gap-4 px-4 py-6 sm:px-6">
          <div className="md:hidden">
            <Drawer state={drawer}>
              <Button variant="outline">
                <Trans id="console.open-menu">Open navigation</Trans>
              </Button>
              <Drawer.Backdrop>
                <Drawer.Content placement="left">
                  <Drawer.Dialog>
                    <Drawer.CloseTrigger
                      aria-label={t({
                        id: "console.close-menu",
                        message: "Close navigation",
                      })}
                    />
                    <Drawer.Header>
                      <Drawer.Heading>
                        <Trans id="console.navigation">
                          Console navigation
                        </Trans>
                      </Drawer.Heading>
                    </Drawer.Header>
                    <Drawer.Body>
                      <ConsoleNavigation session={session} />
                    </Drawer.Body>
                    <Drawer.Footer>
                      <ConsoleActions pending={pending} onLogout={logout} />
                    </Drawer.Footer>
                  </Drawer.Dialog>
                </Drawer.Content>
              </Drawer.Backdrop>
            </Drawer>
          </div>
          <span className="min-w-0 text-lg font-semibold wrap-anywhere">
            {instanceName}
          </span>
        </header>
        <main className="flex min-w-0 flex-col gap-6 px-4 pb-8 sm:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export function ConsoleLayout() {
  const queryClient = useQueryClient();
  const router = useRouter();
  const routeLoading = useRouterState({ select: (state) => state.isLoading });
  const [leaving, setLeaving] = useState(false);
  const expiryHandled = useRef(false);
  const logoutStarted = useRef(false);
  const session = useQuery({
    ...sessionQueryOptions(),
    enabled: !leaving,
    refetchOnMount: false,
    refetchOnWindowFocus: "always",
  });
  const logout = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      flushSync(() => setLeaving(true));
      await clearSessionQueries(queryClient);
      queryClient.getMutationCache().clear();
      window.location.replace("/login");
    },
    onError: () => {
      logoutStarted.current = false;
    },
  });

  useEffect(() => {
    if (
      session.data !== null ||
      leaving ||
      logout.isPending ||
      expiryHandled.current
    )
      return;
    expiryHandled.current = true;
    setLeaving(true);
    notificationQueue.add({
      title: (
        <Trans id="console.session-expired">
          Your session has expired. Sign in again.
        </Trans>
      ),
    });
    void clearSessionQueries(queryClient).then(() => {
      queryClient.getMutationCache().clear();
      return router.navigate({ to: "/login", replace: true });
    });
  }, [session.data, leaving, logout.isPending, queryClient, router]);

  useEffect(() => {
    if (leaving) return;
    const revalidate = () => {
      void queryClient.refetchQueries(
        {
          queryKey: sessionQueryOptions().queryKey,
          exact: true,
          type: "active",
        },
        { cancelRefetch: false },
      );
    };
    window.addEventListener("focus", revalidate);
    return () => window.removeEventListener("focus", revalidate);
  }, [leaving, queryClient]);

  if (
    leaving ||
    routeLoading ||
    session.isFetching ||
    session.isPending ||
    session.data === null
  ) {
    return <RoutePending />;
  }
  if (session.isError) return <RouteError error={session.error} />;
  return (
    <ConsoleShell
      session={session.data}
      pending={logout.isPending}
      onLogout={() => {
        if (logoutStarted.current) return;
        logoutStarted.current = true;
        logout.mutate();
      }}
    />
  );
}
