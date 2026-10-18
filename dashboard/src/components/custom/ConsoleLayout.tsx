import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  type ActiveOptions,
  Link,
  Outlet,
  useNavigate,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import {
  AppWindow,
  Blocks,
  ChevronsUpDown,
  FileKey,
  House,
  Languages,
  Layers,
  LogIn,
  LogOut,
  type LucideIcon,
  MailPlus,
  MonitorSmartphone,
  Palette,
  Route,
  ScrollText,
  SearchIcon,
  Settings,
  ShieldCheck,
  UserRound,
  Users,
} from "lucide-react";
import { type ReactNode, useEffect, useId } from "react";
import type { components } from "@/api/generated/schema";
import { logoutMutationOptions } from "@/api/mutations";
import {
  clearSessionQueries,
  managedApplicationsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import {
  ConsoleCrumbProvider,
  useConsoleCrumbLabel,
} from "@/components/custom/console-crumb";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { LanguageMenuItems } from "@/components/custom/LanguageMenu";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { SudoDialog } from "@/components/custom/SudoDialog";
import { ThemeMenuItems } from "@/components/custom/ThemeSelect";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Kbd } from "@/components/ui/kbd";
import {
  Menu,
  MenuItem,
  MenuLinkItem,
  MenuPopup,
  MenuSeparator,
  MenuSub,
  MenuSubPopup,
  MenuSubTrigger,
  MenuTrigger,
} from "@/components/ui/menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import type { FileRouteTypes } from "@/routeTree.gen";

type Session = components["schemas"]["SessionView"];

/**
 * The console's sections, in the order the sidebar lists them. The header names
 * the section you are in, so each label exists once: the string a member clicks
 * and the title they land on come from the same place.
 */
type ConsoleSection = {
  path: FileRouteTypes["to"];
  icon: LucideIcon;
  title: MessageDescriptor;
};

const consoleHome: ConsoleSection = {
  path: "/",
  icon: House,
  title: msg({ id: "console.home", message: "Console home" }),
};

const accountSections: ConsoleSection[] = [
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
 *
 * `group` names the sidebar heading an entry sits under. The list stays flat and
 * in sidebar order, so the visibility rule and its tests never see the groups;
 * the sidebar splits it by `managementGroups` only when it draws.
 */
type SectionVisibility = "admin" | "oidc" | "saml" | "forwardAuth";

type ManagementGroup = "directory" | "applications" | "system";

const adminSections: (ConsoleSection & {
  group: ManagementGroup;
  visibleTo: SectionVisibility;
})[] = [
  {
    path: "/admin/users",
    group: "directory",
    icon: Users,
    title: msg({ id: "console.admin.users", message: "Users" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/groups",
    group: "directory",
    icon: Layers,
    title: msg({ id: "console.admin.groups", message: "User groups" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/invitations",
    group: "directory",
    icon: MailPlus,
    title: msg({ id: "console.admin.invitations", message: "Invitations" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/identity-providers",
    group: "directory",
    icon: LogIn,
    title: msg({ id: "console.admin.federation", message: "Federation" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/oidc-applications",
    group: "applications",
    icon: Blocks,
    title: msg({
      id: "console.admin.oidc-applications",
      message: "OIDC applications",
    }),
    visibleTo: "oidc",
  },
  {
    path: "/admin/saml-applications",
    group: "applications",
    icon: FileKey,
    title: msg({
      id: "console.admin.saml-applications",
      message: "SAML applications",
    }),
    visibleTo: "saml",
  },
  {
    path: "/admin/forward-auth-apps",
    group: "applications",
    icon: Route,
    title: msg({
      id: "console.admin.forward-auth-apps",
      message: "Forward-auth applications",
    }),
    visibleTo: "forwardAuth",
  },
  {
    path: "/admin/logs",
    group: "system",
    icon: ScrollText,
    title: msg({ id: "console.admin.logs", message: "Logs" }),
    visibleTo: "admin",
  },
  {
    path: "/admin/settings",
    group: "system",
    icon: Settings,
    title: msg({ id: "console.admin.settings", message: "Settings" }),
    visibleTo: "admin",
  },
];

/** The management headings, in the order the sidebar draws them. */
const managementGroups: { id: ManagementGroup; title: MessageDescriptor }[] = [
  {
    id: "directory",
    title: msg({ id: "console.group.directory", message: "Directory" }),
  },
  {
    id: "applications",
    title: msg({ id: "console.group.applications", message: "Applications" }),
  },
  {
    id: "system",
    title: msg({ id: "console.group.system", message: "System" }),
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

const pageNotFound = msg({ id: "route.not-found", message: "Page not found" });

function withoutTrailingSlash(path: string) {
  return path.length > 1 && path.endsWith("/") ? path.slice(0, -1) : path;
}

/** A count shown beside an entry, keyed by the entry's path. */
type NavigationCounts = Partial<Record<ConsoleSection["path"], number>>;

function NavItem({
  section,
  activePath,
  count,
  activeOptions,
}: {
  section: ConsoleSection;
  activePath: string;
  count: number | undefined;
  activeOptions?: ActiveOptions;
}) {
  const { i18n } = useLingui();
  const Icon = section.icon;
  return (
    <SidebarMenuItem>
      {/* A router link, so an entry is a real href that opens in a new tab and
          the router marks the current page with aria-current. */}
      <SidebarMenuButton
        isActive={isActiveSection(section.path, activePath)}
        render={<Link to={section.path} activeOptions={activeOptions} />}
      >
        <Icon aria-hidden="true" />
        <span>{i18n._(section.title)}</span>
      </SidebarMenuButton>
      {count !== undefined && <SidebarMenuBadge>{count}</SidebarMenuBadge>}
    </SidebarMenuItem>
  );
}

/**
 * A heading and the entries under it. The heading is not a heading element:
 * the page's own title starts the outline, and the sidebar is not part of it.
 */
function NavGroup({
  title,
  children,
}: {
  title?: ReactNode;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <SidebarGroup>
      {title && <SidebarGroupLabel id={id}>{title}</SidebarGroupLabel>}
      <SidebarMenu aria-labelledby={title ? id : undefined}>
        {children}
      </SidebarMenu>
    </SidebarGroup>
  );
}

export function ConsoleNavigation({
  managementSections,
  counts = {},
}: {
  /**
   * The management entries this account may see, already filtered. The caller
   * decides, because the answer depends on what the account manages, and the
   * loader that guards the routes asks the same question.
   */
  managementSections: typeof adminSections;
  counts?: NavigationCounts;
}) {
  const { i18n, t } = useLingui();
  const activePath = useRouterState({ select: (s) => s.location.pathname });
  const entry = (section: ConsoleSection, activeOptions?: ActiveOptions) => (
    <NavItem
      key={section.path}
      section={section}
      activePath={activePath}
      count={counts[section.path]}
      activeOptions={activeOptions}
    />
  );
  return (
    <nav
      aria-label={t({
        id: "console.navigation",
        message: "Console navigation",
      })}
      className="flex flex-col gap-2"
    >
      <NavGroup>{entry(consoleHome, { exact: true })}</NavGroup>
      <NavGroup title={<Trans id="console.account">Your account</Trans>}>
        {accountSections.map((section) => entry(section))}
      </NavGroup>
      {/* A heading with nothing under it is left out whole, so a member who
          manages only applications sees that one group. */}
      {managementGroups.map((group) => {
        const sections = managementSections.filter(
          (section) => section.group === group.id,
        );
        if (sections.length === 0) return null;
        return (
          <NavGroup key={group.id} title={i18n._(group.title)}>
            {sections.map((section) => entry(section))}
          </NavGroup>
        );
      })}
    </nav>
  );
}

/** Which install this console belongs to, above the navigation. */
function ConsoleIdentity() {
  const { name, iconUrl } = useInstanceBranding();
  return (
    <SidebarHeader>
      <div className="flex min-w-0 items-center gap-2 p-2">
        <Avatar className="size-8 rounded-lg">
          <AvatarImage src={iconUrl} alt="" />
          <AvatarFallback className="rounded-lg bg-primary font-semibold text-primary-foreground text-sm">
            {name.slice(0, 1)}
          </AvatarFallback>
        </Avatar>
        <div className="grid min-w-0 flex-1 text-left leading-tight">
          <span className="truncate font-semibold">{name}</span>
          <span className="truncate text-muted-foreground text-xs">
            {window.location.host}
          </span>
        </div>
      </div>
    </SidebarHeader>
  );
}

/**
 * The signed-in account at the foot of the sidebar: who you are and, in its
 * menu, the account's pages, the language and theme choices, and the way out.
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
  const role =
    session.role === "admin"
      ? t({ id: "console.role.admin", message: "Admin" })
      : t({ id: "console.role.user", message: "User" });
  return (
    <SidebarFooter>
      <SidebarMenu>
        <SidebarMenuItem>
          <Menu>
            <MenuTrigger render={<SidebarMenuButton size="lg" />}>
              <Avatar className="size-8 rounded-lg">
                {session.avatarUrl && (
                  <AvatarImage src={session.avatarUrl} alt="" />
                )}
                <AvatarFallback className="rounded-lg">
                  {(session.displayName || session.username).slice(0, 1)}
                </AvatarFallback>
              </Avatar>
              <div className="grid min-w-0 flex-1 text-left leading-tight">
                <span className="truncate font-medium">
                  {session.displayName}
                </span>
                <span className="truncate text-muted-foreground text-xs">
                  {role}
                </span>
              </div>
              <ChevronsUpDown
                className="ms-auto opacity-60"
                aria-hidden="true"
              />
            </MenuTrigger>
            <MenuPopup
              align="start"
              side="top"
              className="w-(--anchor-width) min-w-56"
            >
              <MenuLinkItem render={<Link to="/profile" />}>
                <UserRound aria-hidden="true" />
                <Trans id="console.profile">Profile</Trans>
              </MenuLinkItem>
              <MenuLinkItem render={<Link to="/security" />}>
                <ShieldCheck aria-hidden="true" />
                <Trans id="console.security">Security</Trans>
              </MenuLinkItem>
              <MenuSeparator />
              <MenuSub>
                <MenuSubTrigger>
                  <Languages aria-hidden="true" />
                  <Trans id="language.label">Language</Trans>
                </MenuSubTrigger>
                <MenuSubPopup>
                  <LanguageMenuItems />
                </MenuSubPopup>
              </MenuSub>
              <MenuSub>
                <MenuSubTrigger>
                  <Palette aria-hidden="true" />
                  <Trans id="theme.label">Theme</Trans>
                </MenuSubTrigger>
                <MenuSubPopup>
                  <ThemeMenuItems />
                </MenuSubPopup>
              </MenuSub>
              <MenuSeparator />
              <MenuItem disabled={pending} onClick={onLogout}>
                <LogOut aria-hidden="true" />
                <Trans id="console.logout">Sign out</Trans>
              </MenuItem>
            </MenuPopup>
          </Menu>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarFooter>
  );
}

/** Closes the narrow screen's sheet when the reader moves to another page. */
function CloseSheetOnNavigate() {
  const router = useRouter();
  const { setOpenMobile } = useSidebar();
  useEffect(
    () => router.subscribe("onBeforeNavigate", () => setOpenMobile(false)),
    [router, setOpenMobile],
  );
  return null;
}

/**
 * The header's trail: the section you are in and, when the page names a
 * record, the record. The last item is the current page; on a page about no
 * record it is the section and carries the page's `h1`, otherwise the page's
 * own header does.
 */
function ConsoleBreadcrumb({
  section,
}: {
  section: { title: MessageDescriptor; path?: ConsoleSection["path"] };
}) {
  const { i18n, t } = useLingui();
  const record = useConsoleCrumbLabel();
  const title = i18n._(section.title);
  return (
    <Breadcrumb
      aria-label={t({ id: "console.breadcrumb", message: "Breadcrumb" })}
      className="min-w-0"
    >
      <BreadcrumbList className="flex-nowrap">
        {record === undefined ? (
          <BreadcrumbItem className="min-w-0">
            <h1 className="contents">
              <BreadcrumbPage className="truncate">{title}</BreadcrumbPage>
            </h1>
          </BreadcrumbItem>
        ) : (
          <>
            <BreadcrumbItem className="max-sm:hidden">
              <BreadcrumbLink
                render={section.path ? <Link to={section.path} /> : undefined}
              >
                {title}
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator className="max-sm:hidden" />
            <BreadcrumbItem className="min-w-0">
              <BreadcrumbPage className="truncate">{record}</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        )}
      </BreadcrumbList>
    </Breadcrumb>
  );
}

function ConsoleShell({
  session,
  pendingLogout,
  onLogout,
  onSearch,
}: {
  session: Session;
  pendingLogout: boolean;
  onLogout: () => void;
  onSearch?: () => void;
}) {
  const { t } = useLingui();
  const activePath = useRouterState({ select: (s) => s.location.pathname });
  // An address with no page of its own stops matching at the deepest route
  // that exists, which then draws the not-found view in its outlet.
  const matchedPath = useRouterState({
    select: (s) => s.matches.at(-1)?.pathname,
  });
  const managementSections = useManagementSections(session);
  const notFound =
    matchedPath !== undefined &&
    withoutTrailingSlash(matchedPath) !== withoutTrailingSlash(activePath);
  const section: ConsoleSection | undefined = notFound
    ? undefined
    : [...managementSections, consoleHome, ...accountSections].find((entry) =>
        isActiveSection(entry.path, activePath),
      );
  const crumbSection = notFound
    ? { title: pageNotFound }
    : (section ?? consoleHome);

  return (
    <SidebarProvider>
      <CloseSheetOnNavigate />
      <Sidebar
        variant="inset"
        mobileTitle={t({
          id: "console.navigation",
          message: "Console navigation",
        })}
        mobileDescription={null}
      >
        <ConsoleIdentity />
        <SidebarContent>
          <ConsoleNavigation managementSections={managementSections} />
        </SidebarContent>
        <ConsoleAccount
          session={session}
          pending={pendingLogout}
          onLogout={onLogout}
        />
      </Sidebar>
      {/* Content sits on the inset panel, so a pinned table header paints that
          surface rather than the page's. */}
      <SidebarInset className="min-w-0 [--content-surface:var(--background)]">
        <header className="sticky top-[var(--app-sticky-offset)] z-10 flex h-14 shrink-0 items-center gap-2 rounded-t-xl border-b bg-background/80 px-4 backdrop-blur">
          <SidebarTrigger
            className="-ms-1"
            aria-label={t({
              id: "console.toggle-sidebar",
              message: "Toggle sidebar",
            })}
          />
          <ConsoleBreadcrumb section={crumbSection} />
          {onSearch && (
            <InputGroup className="ms-auto w-60 max-sm:hidden">
              <InputGroupAddon>
                <SearchIcon aria-hidden="true" />
              </InputGroupAddon>
              <InputGroupInput
                type="search"
                aria-label={t({ id: "console.search", message: "Search" })}
                placeholder={t({ id: "console.search", message: "Search" })}
                readOnly
                onClick={onSearch}
              />
              <InputGroupAddon align="inline-end">
                <Kbd>⌘K</Kbd>
              </InputGroupAddon>
            </InputGroup>
          )}
        </header>
        {/* One measured column, so a wide window reads as a page rather than a
            stretched one; it is also the container the tables measure their
            full-bleed width against. */}
        <div className="@container mx-auto w-full max-w-6xl px-6 py-8 lg:px-10">
          <SectionRegistryProvider>
            <RouteLayoutContext value="console">
              <Outlet />
            </RouteLayoutContext>
          </SectionRegistryProvider>
        </div>
      </SidebarInset>

      {/* One step-up prompt for the whole console; see SudoDialog. */}
      <SudoDialog />
    </SidebarProvider>
  );
}

export function ConsoleLayout({ onSearch }: { onSearch?: () => void } = {}) {
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
    <ConsoleCrumbProvider>
      <ConsoleShell
        session={session}
        pendingLogout={logout.isPending}
        onLogout={logout.mutate}
        onSearch={onSearch}
      />
    </ConsoleCrumbProvider>
  );
}
