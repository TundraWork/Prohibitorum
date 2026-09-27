import {
  Avatar,
  buttonVariants,
  Drawer,
  ScrollShadow,
  type ScrollShadowVisibility,
  useOverlayState,
} from "@heroui/react";
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
import { cva } from "class-variance-authority";
import {
  AppWindow,
  Blocks,
  ChevronDown,
  ChevronUp,
  FileKey,
  House,
  Layers,
  LogIn,
  LogOut,
  type LucideIcon,
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
import { useOverlayScrollbars } from "overlayscrollbars-react";
import { useEffect, useId, useRef, useState } from "react";
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
import { scrollAreaTheme } from "@/components/custom/ScrollArea";
import { SudoDialog } from "@/components/custom/SudoDialog";
import { ThemeSelect } from "@/components/custom/ThemeSelect";
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

/**
 * One navigation entry.
 *
 * An entry is a router `Link` rather than a button, so it is a real `<a href>`:
 * it opens in a new tab from the context menu, a middle click or a modifier
 * click, and the router marks the current page with `aria-current="page"` and
 * `data-status="active"`. It takes the ghost button's look through
 * `buttonVariants`, the way HeroUI styles a link as a button, which keeps the
 * library's focus ring, transitions and cursor.
 *
 * The rail sits on `bg-surface-secondary`, so an entry cannot borrow the
 * console's own `default` gray for its selected state: that token is a step
 * *below* the rail and would read as a hole punched in it. Selection is the
 * accent's soft tint instead, which is the one place in the shell where the
 * product's colour appears, and hover is half of it, so the row a pointer is on
 * previews what choosing it looks like. The selected icon takes
 * `accent-soft-foreground`, the colour HeroUI pairs with that tint, because the
 * plain accent measures under 3:1 on it in dark mode.
 *
 * The label stays `text-foreground` at reduced opacity rather than `text-muted`:
 * on the rail `muted` measures about 4.2:1 in light mode, under the 4.5:1 body
 * floor, while an 85% foreground measures about 10:1 and still sits a clear step
 * below the selected entry.
 *
 * The button styles pull an icon in with `-mx-0.5` and grow it to 20px on small
 * screens. Both are undone here, so every icon keeps 16px and starts 12px into
 * its row, on the same line as the headings and the avatars above and below.
 * The press keeps the library's scale, like every other button in the console.
 */
const navigationItem = cva(
  "group h-11 w-full justify-start gap-3 rounded-field px-3 text-sm font-normal leading-5 text-foreground/85 hover:bg-accent-soft/50 hover:text-foreground focus-visible:ring-offset-surface-secondary md:h-9 data-[status=active]:bg-accent-soft data-[status=active]:font-medium data-[status=active]:text-foreground data-[status=active]:hover:bg-accent-soft",
);

function NavItem({
  path,
  icon: Icon,
  activeOptions,
  children,
}: {
  path: ConsoleSection["path"];
  icon: LucideIcon;
  activeOptions?: ActiveOptions;
  children: React.ReactNode;
}) {
  return (
    <li>
      <Link
        to={path}
        activeOptions={activeOptions}
        className={`${buttonVariants({ variant: "ghost" })} ${navigationItem()}`}
      >
        <Icon
          className="mx-0 size-4 shrink-0 text-foreground/60 transition-colors group-hover:text-foreground/80 group-data-[status=active]:text-accent-soft-foreground"
          aria-hidden="true"
        />
        <span className="truncate">{children}</span>
      </Link>
    </li>
  );
}

/**
 * A heading and the entries under it. The heading is a paragraph the list
 * names itself after rather than a heading element: the header's `h1` starts
 * the page's outline, and the rail is not part of it.
 */
function NavGroup({
  title,
  children,
}: {
  title: React.ReactNode;
  children: React.ReactNode;
}) {
  const id = useId();
  return (
    <>
      <p
        id={id}
        className="px-3 pt-4 pb-1 text-xs font-medium text-foreground/70"
      >
        {title}
      </p>
      <ul aria-labelledby={id} className="flex flex-col gap-0.5">
        {children}
      </ul>
    </>
  );
}

// A constant, so the hook does not reapply the options on every render.
const railScrollbars = {
  scrollbars: { theme: scrollAreaTheme, autoHide: "scroll" },
} as const;

/**
 * The rail's scroll area, drawn the way HeroUI's `Tabs.ListContainer` handles an
 * overflowing tab list: while there is more above or below, that edge carries a
 * control that scrolls most of a screen's worth towards it. Here the control is
 * a full row of the rail's own gray rather than a lone chevron, so it cannot be
 * read as a caret on the entry under it, and a short gradient leads from the
 * strip into the list: the fade that says the list goes on. The gradient takes
 * no pointer events, so the entries under it stay clickable. `ScrollShadow`
 * only reports which edges have more; its own mask is switched off, since the
 * strip already covers and fades that edge.
 *
 * The strips stay out of the tab order, as HeroUI's chevrons do: a keyboard
 * reaches every entry by tabbing, and the rail scrolls to follow focus. A press
 * does not take focus either, since the strip it lands on is removed once that
 * edge is reached and focus would fall back to the page.
 *
 * The scrollbar itself only shows while the rail scrolls. It is wider than the
 * gutter beside the entries, so a bar left on screen would sit against the
 * rows, and the strips already say there is more. OverlayScrollbars draws it
 * with the app's theme and the rail's narrower handle (`data-console-rail` in
 * `styles/index.css`); it takes the `ScrollShadow` as its viewport, so both work
 * on the one scrolling element. The host is a flex row once OverlayScrollbars
 * owns it, so the column lives on the children.
 */
const railScrollEdge = cva(
  "absolute inset-x-0 z-10 h-11 w-full rounded-none bg-surface-secondary text-foreground/60 hover:bg-surface-secondary hover:text-foreground active:bg-surface-secondary md:h-9 before:pointer-events-none before:absolute before:inset-x-0 before:h-6 before:from-surface-secondary before:to-transparent before:content-['']",
  {
    variants: {
      edge: {
        top: "top-0 before:top-full before:bg-linear-to-b",
        bottom: "bottom-0 before:bottom-full before:bg-linear-to-t",
      },
    },
  },
);

function RailScrollEdge({
  edge,
  label,
  onPress,
}: {
  edge: "top" | "bottom";
  label: string;
  onPress: () => void;
}) {
  const Icon = edge === "top" ? ChevronUp : ChevronDown;
  return (
    <Button
      variant="ghost"
      excludeFromTabOrder
      preventFocusOnPress
      className={railScrollEdge({ edge })}
      aria-label={label}
      onPress={onPress}
    >
      <Icon className="mx-0 size-4" aria-hidden="true" />
    </Button>
  );
}

function RailScrollArea({ children }: { children: React.ReactNode }) {
  const { t } = useLingui();
  const hostRef = useRef<HTMLDivElement>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const [visibility, setVisibility] = useState<ScrollShadowVisibility>("none");
  const [initialize, instance] = useOverlayScrollbars({
    options: railScrollbars,
  });

  useEffect(() => {
    const host = hostRef.current;
    const viewport = viewportRef.current;
    if (!host || !viewport) return;
    initialize({ target: host, elements: { viewport, content: viewport } });
    return () => instance()?.destroy();
  }, [initialize, instance]);

  // HeroUI's step: 80% of the visible height, clamped so a press near the end
  // lands flush on it. Reduced motion jumps instead of gliding.
  const scrollBy = (direction: 1 | -1) => {
    const el = viewportRef.current;
    if (!el) return;
    const max = Math.max(0, el.scrollHeight - el.clientHeight);
    const next = Math.min(
      max,
      Math.max(0, el.scrollTop + direction * el.clientHeight * 0.8),
    );
    if (next === el.scrollTop) return;
    const reduced = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    el.scrollTo({ top: next, behavior: reduced ? "auto" : "smooth" });
  };

  const moreAbove = visibility === "top" || visibility === "both";
  const moreBelow = visibility === "bottom" || visibility === "both";

  return (
    <div
      ref={hostRef}
      data-overlayscrollbars-initialize=""
      data-console-rail=""
      className="relative flex min-h-0 min-w-0 flex-1"
    >
      <ScrollShadow
        ref={viewportRef}
        hideScrollBar
        onVisibilityChange={setVisibility}
        className="flex min-w-0 flex-1 flex-col [-webkit-mask-image:none] [mask-image:none]"
      >
        {children}
      </ScrollShadow>
      {moreAbove && (
        <RailScrollEdge
          edge="top"
          label={t({
            id: "console.scroll-up",
            message: "Scroll navigation up",
          })}
          onPress={() => scrollBy(-1)}
        />
      )}
      {moreBelow && (
        <RailScrollEdge
          edge="bottom"
          label={t({
            id: "console.scroll-down",
            message: "Scroll navigation down",
          })}
          onPress={() => scrollBy(1)}
        />
      )}
    </div>
  );
}

function ConsoleNavigation({
  managementSections,
}: {
  /**
   * The management entries this account may see, already filtered. The caller
   * decides, because the answer depends on what the account manages, and the
   * loader that guards the routes asks the same question.
   */
  managementSections: typeof adminSections;
}) {
  const { i18n, t } = useLingui();
  return (
    // Shrinks with the rail so the account footer below stays at its foot,
    // however short the visible area is.
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <RailScrollArea>
        <div className="flex flex-1 flex-col px-2 pt-2 pb-3">
          <nav
            aria-label={t({
              id: "console.navigation",
              message: "Console navigation",
            })}
            className="flex flex-col"
          >
            <ul className="flex flex-col gap-0.5">
              <NavItem
                path={consoleHome.path}
                icon={consoleHome.icon}
                activeOptions={{ exact: true }}
              >
                {i18n._(consoleHome.title)}
              </NavItem>
            </ul>
            <NavGroup title={<Trans id="console.account">Your account</Trans>}>
              {accountSections.map((section) => (
                <NavItem
                  key={section.path}
                  path={section.path}
                  icon={section.icon}
                >
                  {i18n._(section.title)}
                </NavItem>
              ))}
            </NavGroup>
            {/* A heading with nothing under it is left out whole, so a member
                who manages only applications sees that one group. */}
            {managementGroups.map((group) => {
              const sections = managementSections.filter(
                (section) => section.group === group.id,
              );
              if (sections.length === 0) return null;
              return (
                <NavGroup key={group.id} title={i18n._(group.title)}>
                  {sections.map((section) => (
                    <NavItem
                      key={section.path}
                      path={section.path}
                      icon={section.icon}
                    >
                      {i18n._(section.title)}
                    </NavItem>
                  ))}
                </NavGroup>
              );
            })}
          </nav>
        </div>
      </RailScrollArea>
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
    <div className="flex items-center gap-3 px-5 pb-3 pt-5">
      <Avatar className="size-8 shrink-0 rounded-field">
        <Avatar.Image src={iconUrl} alt="" />
        <Avatar.Fallback className="rounded-field">
          {name.slice(0, 1)}
        </Avatar.Fallback>
      </Avatar>
      <span className="truncate text-sm font-semibold tracking-[-0.01em] text-foreground">
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
    <div className="flex items-center gap-3 rounded-field p-2">
      <Avatar className="size-8 shrink-0 rounded-field">
        {session.avatarUrl && <Avatar.Image src={session.avatarUrl} alt="" />}
        <Avatar.Fallback className="rounded-field">
          <UserRound size={20} aria-hidden="true" />
        </Avatar.Fallback>
      </Avatar>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-medium leading-tight text-foreground">
          {session.displayName}
        </span>
        <span className="truncate font-mono text-xs leading-tight text-foreground/60">
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
        <LogOut
          size={16}
          className="shrink-0 text-foreground/60"
          aria-hidden="true"
        />
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
  const drawer = useOverlayState();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const router = useRouter();
  const activePath = useRouterState({ select: (s) => s.location.pathname });
  const managementSections = useManagementSections(session);
  const asideId = useId();
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

  return (
    <div className="flex min-h-[var(--app-viewport-height)]">
      {/* Desktop sidebar wrapper. The rail is pinned here rather than on the
          aside inside: an `overflow-hidden` ancestor is a scroll container, and
          an aside stuck to a box that never scrolls never moves. */}
      <div
        className={`hidden shrink-0 sticky top-[var(--app-sticky-offset)] h-[var(--app-viewport-height)] overflow-hidden transition-[width] duration-200 motion-reduce:transition-none md:block ${
          sidebarCollapsed ? "w-0" : "w-64"
        }`}
      >
        <aside
          id={asideId}
          className={`flex h-full w-64 flex-col border-r border-separator bg-surface-secondary transition-[translate,visibility] duration-200 motion-reduce:transition-none ${
            sidebarCollapsed ? "invisible -translate-x-full" : "translate-x-0"
          }`}
        >
          <ConsoleIdentity />
          <ConsoleNavigation managementSections={managementSections} />
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
        <header className="sticky top-[var(--app-sticky-offset)] z-10 flex h-16 items-center gap-4 border-b border-separator bg-background px-4 sm:px-6">
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
                  {/* The drawer is the rail on a narrow screen, so it takes the
                      rail's gray and names itself as the navigation rather than
                      after the button that opened it. */}
                  <Drawer.Dialog
                    aria-label={t({
                      id: "console.navigation",
                      message: "Console navigation",
                    })}
                    className="w-[300px] max-w-[85vw] bg-surface-secondary p-0 sm:w-[300px]"
                  >
                    <Drawer.CloseTrigger
                      aria-label={t({
                        id: "console.close-menu",
                        message: "Close navigation",
                      })}
                    />
                    <Drawer.Body className="flex flex-col">
                      <ConsoleIdentity />
                      <ConsoleNavigation
                        managementSections={managementSections}
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
            aria-expanded={!sidebarCollapsed}
            aria-controls={asideId}
            onPress={() => setSidebarCollapsed(!sidebarCollapsed)}
            aria-label={t({
              id: "console.toggle-sidebar",
              message: "Toggle sidebar",
            })}
          >
            <PanelLeft size={16} aria-hidden="true" />
          </Button>

          {/* The section in view. The rail above already names the instance, so
              the header names only where you are and says it once: an eyebrow
              repeating the instance would take the size the heading needs. */}
          <h1 className="min-w-0 truncate text-xl font-semibold leading-tight tracking-[-0.01em] text-foreground">
            {i18n._(sectionTitle)}
          </h1>

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
