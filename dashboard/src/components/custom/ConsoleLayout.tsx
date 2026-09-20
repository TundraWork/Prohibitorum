import {
  Avatar,
  Button,
  Drawer,
  Spinner,
  useOverlayState,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Outlet, useRouter, useRouterState } from "@tanstack/react-router";
import { cva } from "class-variance-authority";
import {
  AppWindow,
  House,
  LogOut,
  MonitorSmartphone,
  PanelLeft,
  ShieldCheck,
  UserRound,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import type { components } from "@/api/generated/schema";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import { instanceName } from "@/components/custom/AppLayout";
import { notificationQueue } from "@/components/custom/AppNotifications";
import { LanguageMenu } from "@/components/custom/LanguageMenu";
import { RoutePending } from "@/components/custom/RouteFeedback";
import { ThemeSelect } from "@/components/custom/ThemeSelect";

type Session = components["schemas"]["SessionView"];

const navigationItem = cva(
  "group flex h-9 w-full items-center justify-start gap-3 rounded-field px-2 py-1.5 text-sm leading-5",
  {
    variants: {
      state: {
        available:
          "text-foreground data-[status=active]:bg-default data-[status=active]:font-medium",
        unavailable: "text-muted",
      },
    },
    defaultVariants: { state: "available" },
  },
);

function NavItem({
  active,
  icon: Icon,
  children,
  disabled,
  onPress,
}: {
  active?: boolean;
  icon: React.ComponentType<{ size?: number; className?: string }>;
  children: React.ReactNode;
  disabled?: boolean;
  onPress?: () => void;
}) {
  const state = disabled ? "unavailable" : "available";
  const cls = navigationItem({ state });
  return (
    <Button
      variant="ghost"
      className={cls}
      isDisabled={disabled}
      data-status={active ? "active" : undefined}
      onPress={onPress}
    >
      <Icon
        size={16}
        className={
          disabled
            ? "shrink-0"
            : "shrink-0 text-muted group-data-[status=active]:text-foreground"
        }
        aria-hidden="true"
      />
      <span className="truncate">{children}</span>
    </Button>
  );
}

function ConsoleNavigation({
  session,
  activePath,
  onNavigate,
}: {
  session: Session;
  activePath: string;
  onNavigate: (path: string) => void;
}) {
  const { t } = useLingui();
  return (
    <div className="flex min-w-0 flex-col">
      <div className="px-4 pb-2 pt-4">
        <div className="flex items-center gap-3 px-1 py-1">
          <Avatar className="size-9 shrink-0">
            {session.avatarUrl && (
              <Avatar.Image src={session.avatarUrl} alt="" />
            )}
            <Avatar.Fallback>
              <UserRound size={20} aria-hidden="true" />
            </Avatar.Fallback>
          </Avatar>
          <div className="flex min-w-0 flex-col">
            <span className="text-sm font-medium leading-tight text-foreground">
              {session.displayName}
            </span>
            <span className="text-xs font-medium leading-tight text-muted">
              {session.username}
            </span>
          </div>
        </div>
      </div>
      <div className="flex flex-1 flex-col overflow-y-auto px-3">
        <nav
          aria-label={t({
            id: "console.navigation",
            message: "Console navigation",
          })}
          className="flex flex-col gap-0.5"
        >
          <NavItem
            active={activePath === "/"}
            icon={House}
            onPress={() => onNavigate("/")}
          >
            <Trans id="console.home">Console home</Trans>
          </NavItem>
          <p className="px-2 pb-1 pt-3 text-xs font-medium text-muted">
            <Trans id="console.coming-soon">Coming later</Trans>
          </p>
          <NavItem disabled icon={UserRound}>
            <Trans id="console.profile">Profile</Trans>
          </NavItem>
          <NavItem disabled icon={ShieldCheck}>
            <Trans id="console.security">Security</Trans>
          </NavItem>
          <NavItem disabled icon={AppWindow}>
            <Trans id="console.applications">Applications</Trans>
          </NavItem>
          <NavItem disabled icon={MonitorSmartphone}>
            <Trans id="console.devices">Devices</Trans>
          </NavItem>
        </nav>
      </div>
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
    <div className="flex flex-col gap-0.5 px-3 pb-4 pt-2">
      <Button
        variant="ghost"
        className={navigationItem()}
        isPending={pending}
        onPress={onLogout}
      >
        {pending ? (
          <Spinner size="sm" className="shrink-0" />
        ) : (
          <LogOut
            size={16}
            className="shrink-0 text-muted"
            aria-hidden="true"
          />
        )}
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
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const router = useRouter();
  const activePath = useRouterState({ select: (s) => s.location.pathname });
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
  const navigate = (path: string) => {
    close();
    void router.navigate({ to: path });
  };

  return (
    <div className="flex min-h-dvh">
      {/* Desktop sidebar wrapper */}
      <div
        className={`hidden shrink-0 overflow-hidden transition-[width] duration-200 motion-reduce:transition-none md:block ${
          sidebarCollapsed ? "w-0" : "w-60"
        }`}
      >
        <aside
          className={`sticky top-0 flex h-dvh w-60 flex-col border-r border-separator transition-[translate,visibility] duration-200 motion-reduce:transition-none ${
            sidebarCollapsed ? "invisible -translate-x-full" : "translate-x-0"
          }`}
        >
          <ConsoleNavigation
            session={session}
            activePath={activePath}
            onNavigate={navigate}
          />
          <div className="mt-auto">
            <ConsoleActions pending={pending} onLogout={logout} />
          </div>
        </aside>
      </div>

      {/* Body */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Header */}
        <header className="sticky top-0 z-10 flex h-16 items-center gap-4 bg-background px-6">
          {/* Mobile menu toggle */}
          <div className="md:hidden">
            <Drawer state={drawer}>
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                aria-label={t({
                  id: "console.open-menu",
                  message: "Open navigation",
                })}
              >
                <PanelLeft size={16} aria-hidden="true" />
              </Button>
              <Drawer.Backdrop>
                <Drawer.Content placement="left">
                  <Drawer.Dialog className="w-[300px] max-w-[85vw] p-0 sm:w-[300px]">
                    <Drawer.CloseTrigger
                      aria-label={t({
                        id: "console.close-menu",
                        message: "Close navigation",
                      })}
                    />
                    <Drawer.Body className="flex flex-col p-0">
                      <ConsoleNavigation
                        session={session}
                        activePath={activePath}
                        onNavigate={navigate}
                      />
                      <div className="mt-auto">
                        <ConsoleActions pending={pending} onLogout={logout} />
                      </div>
                    </Drawer.Body>
                  </Drawer.Dialog>
                </Drawer.Content>
              </Drawer.Backdrop>
            </Drawer>
          </div>

          {/* Desktop sidebar trigger */}
          <Button
            isIconOnly
            size="sm"
            variant="ghost"
            className="hidden md:flex"
            onPress={() => setSidebarCollapsed(!sidebarCollapsed)}
            aria-label={t({
              id: "console.toggle-sidebar",
              message: "Toggle sidebar",
            })}
          >
            <PanelLeft size={16} aria-hidden="true" />
          </Button>

          {/* Title */}
          <h1 className="truncate text-xl font-semibold text-foreground">
            {instanceName}
          </h1>

          {/* Spacer */}
          <div className="flex-1" />

          {/* Actions */}
          <div className="flex items-center gap-2">
            <LanguageMenu />
            <ThemeSelect />
          </div>
        </header>

        {/* Main content */}
        <main className="flex min-w-0 flex-1 flex-col gap-6 px-4 pb-8 sm:px-6">
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
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
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
      session !== null ||
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
  }, [session, leaving, logout.isPending, queryClient, router]);

  if (leaving || routeLoading || session === null) {
    return <RoutePending />;
  }
  return (
    <ConsoleShell
      session={session}
      pending={logout.isPending}
      onLogout={() => {
        if (logoutStarted.current) return;
        logoutStarted.current = true;
        logout.mutate();
      }}
    />
  );
}
