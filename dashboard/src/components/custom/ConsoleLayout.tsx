import { Avatar, Drawer, useOverlayState } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  Outlet,
  useNavigate,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { cva } from "class-variance-authority";
import {
  AppWindow,
  Blocks,
  FileKey,
  House,
  Layers,
  LogIn,
  LogOut,
  MailPlus,
  MonitorSmartphone,
  PanelLeft,
  Route,
  ScrollText,
  Settings,
  ShieldCheck,
  UserRound,
  Users,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { components } from "@/api/generated/schema";
import { logoutMutationOptions } from "@/api/mutations";
import {
  clearSessionQueries,
  managedApplicationsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { LanguageMenu } from "@/components/custom/LanguageMenu";
import { ScrollArea } from "@/components/custom/ScrollArea";
import { SudoDialog } from "@/components/custom/SudoDialog";
import { ThemeSelect } from "@/components/custom/ThemeSelect";

type Session = components["schemas"]["SessionView"];

/**
 * The console's sections, in the order the sidebar lists them. The header names
 * the section you are in, so each label exists once: the string a member clicks
 * and the title they land on come from the same place.
 */
const consoleHome = {
  path: "/",
  icon: House,
  title: msg({ id: "console.home", message: "Console home" }),
};

const accountSections = [
  {
    path: "/profile",
    icon: UserRound,
    title: msg({ id: "console.profile", message: "Profile" }),
  },
  {
    path: "/security",
    icon: ShieldCheck,
    title: msg({ id: "console.security", message: "Security" }),
  },
  {
    path: "/apps",
    icon: AppWindow,
    title: msg({
      id: "console.applications",
      message: "Connected applications",
    }),
  },
  {
    path: "/devices",
    icon: MonitorSmartphone,
    title: msg({ id: "console.devices", message: "Devices" }),
  },
];

/**
 * Management sections.
 *
 * `visibleTo` decides which accounts see an entry. Most are for administrators
 * alone; the three application sections are also for an account assigned to
 * manage at least one application of that kind, which is why the entry and the
 * `_protected.admin` loader ask the same question. Federation is administrator
 * only, so it keeps the admin gate.
 *
 * A hidden entry is never the only thing keeping someone out: `_protected.admin`
 * and `_protected.admin._admin` refuse the routes themselves.
 */
type SectionVisibility = "admin" | "oidc" | "saml" | "forwardAuth";

const adminSections: {
  path: string;
  icon: typeof Users;
  title: ReturnType<typeof msg>;
  visibleTo: SectionVisibility;
}[] = [
  {
    path: "/admin/users",
    icon: Users,
    title: msg({ id: "console.admin.users", message: "Users" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/groups",
    icon: Layers,
    title: msg({ id: "console.admin.groups", message: "User groups" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/invitations",
    icon: MailPlus,
    title: msg({ id: "console.admin.invitations", message: "Invitations" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/identity-providers",
    icon: LogIn,
    title: msg({ id: "console.admin.federation", message: "Federation" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/oidc-applications",
    icon: Blocks,
    title: msg({
      id: "console.admin.oidc-applications",
      message: "OIDC applications",
    }),
    visibleTo: "oidc",
  },
  {
    path: "/admin/saml-applications",
    icon: FileKey,
    title: msg({
      id: "console.admin.saml-applications",
      message: "SAML applications",
    }),
    visibleTo: "saml",
  },
  {
    path: "/admin/forward-auth-apps",
    icon: Route as typeof Users,
    title: msg({
      id: "console.admin.forward-auth-apps",
      message: "Forward-auth applications",
    }),
    visibleTo: "forwardAuth",
  },
  {
    path: "/admin/logs",
    icon: ScrollText,
    title: msg({ id: "console.admin.logs", message: "Logs" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/settings",
    icon: Settings,
    title: msg({ id: "console.admin.settings", message: "Settings" }),
    visibleTo: "admin",
  },
];

/**
 * Which management entries one account sees.
 *
 * Split out from the hook because it is the rule, not the read: the tests that
 * matter are about which sections a given combination of role and assignment
 * produces, and they should not have to render a sidebar to ask.
 */
export function visibleManagementSections(
  isAdmin: boolean,
  managed: { oidc: boolean; saml: boolean; forwardAuth: boolean } | undefined,
): typeof adminSections {
  if (isAdmin) return adminSections;
  return adminSections.filter((section) => {
    if (section.visibleTo === "admin") return false;
    return managed?.[section.visibleTo] === true;
  });
}

/**
 * The management entries this account may see.
 *
 * An administrator sees all of them. Anyone else sees only the application
 * sections they have been assigned work in: the server lets them manage those
 * applications, so the entry is theirs, but nothing else in the management area
 * is. Asking the same question the `_protected.admin` loader asks keeps the
 * sidebar and the routes in step.
 *
 * The managed-application read is only made when it can matter; for an admin the
 * answer is already known.
 */
function useManagementSections(session: Session) {
  const isAdmin = session.role === "admin";
  const { data: managed } = useQuery({
    ...managedApplicationsQueryOptions(),
    enabled: !isAdmin,
  });
  return visibleManagementSections(isAdmin, managed);
}

// Prefix matching, so a page's own query strings and any nested route keep the
// section highlighted. `/` only ever matches the console home.
function isActiveSection(path: string, activePath: string) {
  return path === "/" ? activePath === "/" : activePath.startsWith(path);
}

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
  activePath,
  managementSections,
  onNavigate,
}: {
  activePath: string;
  /**
   * The management entries this account may see, already filtered. The caller
   * decides, because the answer depends on what the account manages, and the
   * loader that guards the routes asks the same question.
   */
  managementSections: typeof adminSections;
  onNavigate: (path: string) => void;
}) {
  const { i18n, t } = useLingui();
  return (
    // Shrinks with the rail so the account footer below stays at its foot,
    // however short the visible area is.
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      {/*
        The rail's own scroll area. `ScrollArea` makes this element the
        OverlayScrollbars host, which forces `flex-direction: row` on it, so the
        column and its padding live on the child below instead.
      */}
      <ScrollArea className="flex flex-1 flex-col">
        <div className="flex flex-1 flex-col px-3 pt-2">
          <nav
            aria-label={t({
              id: "console.navigation",
              message: "Console navigation",
            })}
            className="flex flex-col gap-0.5"
          >
            <NavItem
              active={isActiveSection(consoleHome.path, activePath)}
              icon={consoleHome.icon}
              onPress={() => onNavigate(consoleHome.path)}
            >
              {i18n._(consoleHome.title)}
            </NavItem>
            <p className="px-2 pb-1 pt-3 text-xs font-medium text-muted">
              <Trans id="console.account">Your account</Trans>
            </p>
            {accountSections.map((section) => (
              <NavItem
                key={section.path}
                active={isActiveSection(section.path, activePath)}
                icon={section.icon}
                onPress={() => onNavigate(section.path)}
              >
                {i18n._(section.title)}
              </NavItem>
            ))}
            {managementSections.length > 0 && (
              <>
                <p className="px-2 pb-1 pt-3 text-xs font-medium text-muted">
                  <Trans id="console.administration">Administration</Trans>
                </p>
                {managementSections.map((section) => (
                  <NavItem
                    key={section.path}
                    active={isActiveSection(section.path, activePath)}
                    icon={section.icon}
                    onPress={() => onNavigate(section.path)}
                  >
                    {i18n._(section.title)}
                  </NavItem>
                ))}
              </>
            )}
          </nav>
        </div>
      </ScrollArea>
    </div>
  );
}

/**
 * Which install this console belongs to, above the navigation. The header names
 * the section you are in; this names the instance all of it is served from.
 */
function ConsoleIdentity() {
  const { name, iconUrl } = useInstanceBranding();
  return (
    <div className="flex items-center gap-3 px-4 pb-2 pt-4">
      <Avatar className="size-8 shrink-0">
        <Avatar.Image src={iconUrl} alt="" />
        <Avatar.Fallback>{name.slice(0, 1)}</Avatar.Fallback>
      </Avatar>
      <span className="truncate text-sm font-medium text-foreground">
        {name}
      </span>
    </div>
  );
}

/**
 * The signed-in account and the one action it carries, at the foot of every
 * console surface: who you are, and the way out.
 */
function ConsoleAccount({
  session,
  pending,
  onLogout,
}: {
  session: Session;
  pending: boolean;
  onLogout: () => void;
}) {
  const { t } = useLingui();
  return (
    <div className="flex items-center gap-3 p-2">
      <Avatar className="size-8 shrink-0">
        {session.avatarUrl && <Avatar.Image src={session.avatarUrl} alt="" />}
        <Avatar.Fallback>
          <UserRound size={20} aria-hidden="true" />
        </Avatar.Fallback>
      </Avatar>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-medium leading-tight text-foreground">
          {session.displayName}
        </span>
        <span className="truncate text-xs font-medium leading-tight text-muted">
          {session.username}
        </span>
      </div>
      <Button
        isIconOnly
        size="sm"
        variant="ghost"
        isPending={pending}
        onPress={onLogout}
        aria-label={t({ id: "console.logout", message: "Sign out" })}
      >
        <LogOut size={16} className="shrink-0 text-muted" aria-hidden="true" />
      </Button>
    </div>
  );
}

function ConsoleShell({
  session,
  pendingLogout,
  onLogout,
}: {
  session: Session;
  pendingLogout: boolean;
  onLogout: () => void;
}) {
  const { i18n, t } = useLingui();
  const { name: instanceName } = useInstanceBranding();
  const drawer = useOverlayState();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const router = useRouter();
  const activePath = useRouterState({ select: (s) => s.location.pathname });
  const managementSections = useManagementSections(session);
  const sectionTitle =
    [...managementSections, consoleHome, ...accountSections].find((section) =>
      isActiveSection(section.path, activePath),
    )?.title ?? consoleHome.title;
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
    <div className="flex min-h-[var(--app-viewport-height)]">
      {/* Desktop sidebar wrapper. The rail is pinned here rather than on the
          aside inside: an `overflow-hidden` ancestor is a scroll container, and
          an aside stuck to a box that never scrolls never moves. */}
      <div
        className={`hidden shrink-0 sticky top-[var(--app-sticky-offset)] h-[var(--app-viewport-height)] overflow-hidden transition-[width] duration-200 motion-reduce:transition-none md:block ${
          sidebarCollapsed ? "w-0" : "w-60"
        }`}
      >
        <aside
          className={`flex h-full w-60 flex-col border-r border-separator transition-[translate,visibility] duration-200 motion-reduce:transition-none ${
            sidebarCollapsed ? "invisible -translate-x-full" : "translate-x-0"
          }`}
        >
          <ConsoleIdentity />
          <ConsoleNavigation
            activePath={activePath}
            managementSections={managementSections}
            onNavigate={navigate}
          />
          <div className="mt-auto px-3 pb-4 pt-3">
            <ConsoleAccount
              session={session}
              pending={pendingLogout}
              onLogout={logout}
            />
          </div>
        </aside>
      </div>

      {/* Body */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Header */}
        <header className="sticky top-[var(--app-sticky-offset)] z-10 flex h-16 items-center gap-4 bg-background px-6">
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
                      <ConsoleIdentity />
                      <ConsoleNavigation
                        activePath={activePath}
                        managementSections={managementSections}
                        onNavigate={navigate}
                      />
                      <div className="mt-auto px-3 pb-4 pt-3">
                        <ConsoleAccount
                          session={session}
                          pending={pendingLogout}
                          onLogout={logout}
                        />
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

          {/* Title: which instance this console belongs to, above the section
              in view. */}
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate text-xs uppercase tracking-wider leading-tight text-muted">
              {instanceName}
            </span>
            <h1 className="truncate text-lg font-semibold leading-tight text-foreground">
              {i18n._(sectionTitle)}
            </h1>
          </div>

          {/* Spacer */}
          <div className="flex-1" />

          {/* Actions */}
          <div className="flex items-center gap-2">
            <LanguageMenu />
            <ThemeSelect />
          </div>
        </header>

        {/* Main content: one measured column, so a wide window reads as a page
            rather than a stretched one. It is also the container the tables
            measure their full-bleed width against. */}
        <main className="@container flex min-w-0 flex-1 flex-col px-4 pt-2 pb-4 sm:px-6">
          <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
            <Outlet />
          </div>
        </main>
      </div>

      {/* One step-up prompt for the whole console; see SudoDialog. */}
      <SudoDialog />
    </div>
  );
}

export function ConsoleLayout() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
    refetchOnWindowFocus: "always",
  });
  const logout = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      queryClient.getMutationCache().clear();
      await navigate({ to: "/login" });
    },
  });

  // session must be not null, just a type guard
  if (session === null) {
    return null;
  }

  return (
    <ConsoleShell
      session={session}
      pendingLogout={logout.isPending}
      onLogout={logout.mutate}
    />
  );
}
