import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import {
  ChevronDown,
  LayoutDashboard,
  LogOut,
  ShieldCheck,
  UserRound,
} from "lucide-react";
import type { CSSProperties } from "react";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import backdropDark from "@/assets/launcher-backdrop-dark.webp";
import backdropLight from "@/assets/launcher-backdrop-light.webp";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { InstanceIdentity } from "@/components/custom/InstanceIdentity";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { MaintenanceBanner } from "@/components/custom/MaintenanceBanner";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
  Menu,
  MenuItem,
  MenuLinkItem,
  MenuPopup,
  MenuSeparator,
  MenuTrigger,
} from "@/components/ui/menu";

/**
 * The launcher's page: the sign-in pages' toolbar, here with the account on
 * its own capsule, over a window illustration that fades in from the top-left
 * corner of the window (under the frosted capsules, so it has no top edge) and
 * grows with it, and the launcher in one centred column. It is not the console: no
 * sidebar, because a member comes here to leave for an application, and the
 * console is a step away in the account menu.
 */
export function LauncherLayout() {
  const { loginAppearance } = useInstanceBranding();
  return (
    <div className="relative flex min-h-(--app-viewport-height) flex-col overflow-x-clip">
      <MaintenanceBanner />
      <div
        aria-hidden="true"
        style={
          {
            "--backdrop-light": `url(${backdropLight})`,
            "--backdrop-dark": `url(${backdropDark})`,
          } as CSSProperties
        }
        className="launcher-backdrop pointer-events-none absolute start-0 top-0 aspect-3/4 w-[clamp(22rem,34vw,42rem)] max-w-[85vw] select-none"
      />
      <div className="relative">
        <AppToolbar
          surface={loginAppearance.capsules}
          showThemeSelect
          account={<AccountMenu />}
        >
          <Link
            to="/"
            className="flex min-w-0 items-center gap-2 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <InstanceIdentity />
          </Link>
        </AppToolbar>
      </div>
      <main className="relative mx-auto w-full max-w-5xl px-5 pt-6 pb-16 sm:px-8 sm:pt-10">
        <Outlet />
      </main>
    </div>
  );
}

/**
 * Who is signed in, and from them the way to the console: the account's own
 * pages, the management area for an administrator, and signing out.
 */
function AccountMenu() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  const logout = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      queryClient.getMutationCache().clear();
      await navigate({ to: "/login" });
    },
  });
  if (session === null) return null;
  const isAdmin = session.role === "admin";
  const avatar = (size: string) => (
    <Avatar className={size}>
      {session.avatarUrl && <AvatarImage src={session.avatarUrl} alt="" />}
      <AvatarFallback>
        {(session.displayName || session.username).slice(0, 1)}
      </AvatarFallback>
    </Avatar>
  );
  return (
    <Menu>
      <MenuTrigger
        render={
          <button
            type="button"
            aria-label={t({ id: "launcher.account", message: "Your account" })}
            className="flex h-7 cursor-pointer items-center gap-1 rounded-full ps-0.5 pe-1 outline-none transition-opacity hover:opacity-80 focus-visible:ring-2 focus-visible:ring-ring data-popup-open:opacity-80"
          />
        }
      >
        {avatar("size-7")}
        <ChevronDown
          className="size-4 text-muted-foreground"
          aria-hidden="true"
        />
      </MenuTrigger>
      <MenuPopup align="end" className="min-w-60">
        <div className="flex items-center gap-2.5 px-2 py-1.5">
          {avatar("size-8")}
          <div className="grid min-w-0 flex-1 leading-tight">
            <span className="truncate font-medium text-sm">
              {session.displayName}
            </span>
            <span className="truncate font-mono text-muted-foreground text-xs">
              {session.username}
            </span>
          </div>
          <Badge variant="outline">
            {isAdmin ? (
              <Trans id="console.role.admin">Admin</Trans>
            ) : (
              <Trans id="console.role.user">User</Trans>
            )}
          </Badge>
        </div>
        <MenuSeparator />
        <MenuLinkItem render={<Link to="/profile" />}>
          <UserRound aria-hidden="true" />
          <Trans id="console.profile">Profile</Trans>
        </MenuLinkItem>
        <MenuLinkItem render={<Link to="/security" />}>
          <ShieldCheck aria-hidden="true" />
          <Trans id="console.security">Security</Trans>
        </MenuLinkItem>
        {isAdmin && (
          <MenuLinkItem render={<Link to="/admin" />}>
            <LayoutDashboard aria-hidden="true" />
            <Trans id="launcher.admin">Administration</Trans>
          </MenuLinkItem>
        )}
        <MenuSeparator />
        <MenuItem disabled={logout.isPending} onClick={() => logout.mutate()}>
          <LogOut aria-hidden="true" />
          <Trans id="console.logout">Sign out</Trans>
        </MenuItem>
      </MenuPopup>
    </Menu>
  );
}
