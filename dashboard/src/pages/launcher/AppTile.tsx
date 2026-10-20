import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { Link } from "@tanstack/react-router";
import {
  ArrowUpRight,
  Check,
  Link2,
  MoreVertical,
  SlidersHorizontal,
  Trash2,
} from "lucide-react";
import { type CSSProperties, useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  notifyFailure,
  notifySuccess,
} from "@/components/custom/AppNotifications";
import { initialsOf } from "@/components/custom/EntityAvatar";
import {
  Menu,
  MenuItem,
  MenuLinkItem,
  MenuPopup,
  MenuSeparator,
  MenuTrigger,
} from "@/components/ui/menu";
import { appTint } from "@/lib/app-color";
import {
  type AppKind,
  ProtocolCapsule,
} from "@/pages/launcher/ProtocolCapsule";

export type LaunchpadApp = components["schemas"]["LaunchpadApp"];

/** Whether the server's kind is one this page knows how to draw and manage. */
export function isAppKind(kind: string): kind is AppKind {
  return kind === "oidc" || kind === "saml" || kind === "forward_auth";
}

/**
 * One application on the launcher, as a tile in the console's frame material:
 * a ringed bezel holding a cover tinted from the application's own colour with
 * its icon at the centre, and a band under it with its name. The whole tile
 * opens the application in a new tab; two capsules ride the cover's top
 * corners, how it signs the member in on the left and what else there is to do
 * on the right, above the tile's link so they work without opening it.
 */
export function AppTile({
  app,
  kind,
  granted,
  canRevoke,
  canManage,
  onRevoke,
  order,
}: {
  app: LaunchpadApp;
  kind: AppKind;
  /** The member has allowed this application to read their profile. */
  granted: boolean;
  canRevoke: boolean;
  canManage: boolean;
  onRevoke: () => void;
  /** The tile's place on the pad, which staggers its arrival. */
  order: number;
}) {
  const { t } = useLingui();

  return (
    <li
      className="group/tile relative"
      style={{ animationDelay: `${Math.min(order, 12) * 35}ms` }}
    >
      {/* An opaque canvas under the frame's veil: the tile sits over the
          window picture, and its name must not. */}
      <div className="flex h-full flex-col rounded-[22px] bg-sidebar bg-[linear-gradient(var(--frame-band),var(--frame-band))] p-1.5 shadow-xs/5 ring-1 ring-border transition-[translate,box-shadow] duration-200 ease-out group-hover/tile:-translate-y-1 group-hover/tile:shadow-lg/8 motion-reduce:transition-none motion-reduce:group-hover/tile:translate-y-0">
        <div
          style={coverStyle(app)}
          className="app-cover-tint relative aspect-4/3 overflow-hidden rounded-2xl border border-black/6 bg-clip-padding dark:border-white/6"
        >
          <div className="absolute inset-0 grid place-items-center">
            <div className="@container aspect-square h-[64%] transition-transform duration-300 ease-out group-hover/tile:scale-105 motion-reduce:transition-none motion-reduce:group-hover/tile:scale-100">
              <AppIcon app={app} />
            </div>
          </div>
        </div>
        {/* Two lines before an ellipsis: what tells two applications apart is
            often at the end of the name ("Prometheus (dev)"). */}
        <div className="flex min-h-10 items-center gap-1.5 px-2.5 pt-1.5">
          <span
            title={app.name}
            className="line-clamp-2 min-w-0 flex-1 wrap-break-word font-medium text-sm leading-5"
          >
            {app.name}
          </span>
          <ArrowUpRight
            aria-hidden="true"
            className="size-4 shrink-0 text-muted-foreground opacity-0 transition-opacity group-focus-within/tile:opacity-100 group-hover/tile:opacity-100"
          />
          {granted && (
            <span
              data-slot="access-granted"
              className="shrink-0 text-success-foreground"
            >
              <Check className="size-4" aria-hidden="true" />
              <span className="sr-only">
                <Trans id="launcher.granted">Access granted</Trans>
              </span>
            </span>
          )}
        </div>
      </div>

      <a
        href={app.launchUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="absolute inset-0 z-1 rounded-[22px] outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
      >
        <span className="sr-only">
          <Trans id="launcher.open">Open {app.name} in a new tab</Trans>
        </span>
      </a>

      <ProtocolCapsule kind={kind} className="absolute start-3 top-3 z-2" />

      <div className="absolute end-3 top-3 z-2">
        <Menu>
          <MenuTrigger
            render={
              <button
                type="button"
                aria-label={t({
                  id: "launcher.more",
                  message: `More for ${app.name}`,
                })}
                className="grid size-7 cursor-pointer place-items-center rounded-full bg-background/85 text-muted-foreground shadow-xs/5 outline-none ring-1 ring-black/8 backdrop-blur-sm transition-colors hover:bg-background hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-popup-open:bg-background data-popup-open:text-foreground dark:ring-white/12"
              />
            }
          >
            <MoreVertical className="size-4" aria-hidden="true" />
          </MenuTrigger>
          <MenuPopup align="end" className="min-w-48">
            <MenuItem onClick={() => copyLink(app.launchUrl)}>
              <Link2 aria-hidden="true" />
              <Trans id="launcher.copy">Copy link</Trans>
            </MenuItem>
            {canManage && <ManageItem app={app} kind={kind} />}
            {canRevoke && (
              <>
                <MenuSeparator />
                <MenuItem variant="destructive" onClick={onRevoke}>
                  <Trash2 aria-hidden="true" />
                  <Trans id="launcher.revoke">Remove access</Trans>
                </MenuItem>
              </>
            )}
          </MenuPopup>
        </Menu>
      </div>
    </li>
  );
}

/** The cover's tint from the application's own colour (`lib/app-color.ts`). */
export function coverStyle(app: LaunchpadApp): CSSProperties {
  const tint = appTint(app.name, app.accentColor);
  return {
    "--app-h": tint.hue.toFixed(1),
    "--app-c": tint.chroma.toFixed(4),
  } as CSSProperties;
}

/**
 * The application's own icon, contained and never cropped so a transparent
 * logo keeps its shape on the tint; without one, or when it fails to load, its
 * monogram.
 */
export function AppIcon({ app }: { app: LaunchpadApp }) {
  const [failed, setFailed] = useState<string | undefined>(undefined);
  if (app.iconUrl !== undefined && app.iconUrl !== failed) {
    return (
      <img
        src={app.iconUrl}
        alt=""
        loading="lazy"
        onError={() => setFailed(app.iconUrl)}
        className="size-full object-contain"
      />
    );
  }
  return (
    <div className="app-monogram grid size-full place-items-center rounded-[22%] shadow-sm ring-1 ring-black/10 ring-inset">
      <span
        aria-hidden="true"
        className="select-none font-semibold text-[46cqi] text-white/92 leading-none [text-shadow:0_1px_8px_oklch(0_0_0/0.3)]"
      >
        {initialsOf(app.name).slice(0, 1)}
      </span>
    </div>
  );
}

/** For an administrator, the way to the application's settings. */
function ManageItem({ app, kind }: { app: LaunchpadApp; kind: AppKind }) {
  const label = (
    <>
      <SlidersHorizontal aria-hidden="true" />
      <Trans id="launcher.manage">Manage application</Trans>
    </>
  );
  switch (kind) {
    case "oidc":
      return (
        <MenuLinkItem
          render={
            <Link
              to="/admin/oidc-applications/$clientId"
              params={{ clientId: app.id }}
            />
          }
        >
          {label}
        </MenuLinkItem>
      );
    case "saml":
      return (
        <MenuLinkItem
          render={
            <Link to="/admin/saml-applications/$id" params={{ id: app.id }} />
          }
        >
          {label}
        </MenuLinkItem>
      );
    case "forward_auth":
      return (
        <MenuLinkItem
          render={
            <Link
              to="/admin/forward-auth-apps/$clientId"
              params={{ clientId: app.id }}
            />
          }
        >
          {label}
        </MenuLinkItem>
      );
  }
}

/**
 * Copies where the application opens. A SAML application opens through this
 * instance by a relative address, so the link is made absolute to work pasted
 * anywhere.
 */
function copyLink(launchUrl: string) {
  const link = new URL(launchUrl, window.location.origin).href;
  navigator.clipboard.writeText(link).then(
    () => notifySuccess(msg({ id: "launcher.copied", message: "Link copied" })),
    () =>
      notifyFailure(
        msg({
          id: "launcher.copy_failed",
          message: "The browser did not allow copying the link.",
        }),
      ),
  );
}
