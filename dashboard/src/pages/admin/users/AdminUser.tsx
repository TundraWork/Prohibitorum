import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { accountQueryOptions } from "@/api/queries";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import {
  ObjectHeader,
  type ObjectStatus,
} from "@/components/custom/ObjectHeader";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { AccountAccessLegacy } from "@/pages/admin/users/AccountAccessLegacy";
import { AccountDangerSection } from "@/pages/admin/users/AccountDangerSection";
import { AccountProfileSection } from "@/pages/admin/users/AccountProfileSection";
import { AccountRegistrationSection } from "@/pages/admin/users/AccountRegistrationSection";
import { Route } from "@/routes/_protected.admin._admin.users_.$id";

/**
 * One account in the management area: who it is at the top, then a column of
 * sections with an outline beside them.
 *
 * The profile form comes first, then how the account gets in and the link that
 * sets that up, and last the changes that stop it. An older `?tab=` link, from
 * when the page was three tabs, scrolls to the section of that name. The record
 * is read once here and passed down, since the route loader already has it.
 */
export function AdminUser() {
  const { t, i18n } = useLingui();
  const { id } = Route.useParams();
  const { tab } = Route.useSearch();
  const accountId = Number(id);
  const { data: account } = useSuspenseQuery(accountQueryOptions(accountId));

  useEffect(() => {
    // The profile is already under the header, so only the sections below it
    // have anywhere to scroll to.
    if (tab === "profile") return;
    // After the router's own scroll reset, which runs once the page has rendered.
    const timer = window.setTimeout(
      () => document.getElementById(tab)?.scrollIntoView(),
      0,
    );
    return () => window.clearTimeout(timer);
  }, [tab]);

  const name = account.displayName || account.username;
  const joined = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
  }).format(new Date(account.createdAt));
  const role = account.role;
  const known = role === "admin" || role === "user";
  const statuses: ObjectStatus[] = [];
  if (role === "admin") statuses.push("admin");
  if (role === "user") statuses.push("user");
  statuses.push(account.disabled ? "disabled" : "enabled");

  return (
    <AppDetailLayout
      header={
        <ObjectHeader
          icon={
            <EntityAvatar
              size="xl"
              shape="round"
              iconUrl={account.avatarUrl}
              name={name}
            />
          }
          title={name}
          statuses={statuses}
          identifier={{
            value: `@${account.username}`,
            label: t({
              id: "admin.user.copy-username",
              message: "Copy the username",
            }),
          }}
          facts={[
            ...(known
              ? []
              : [
                  <Trans key="role" id="admin.user.role.fact">
                    Role {role}
                  </Trans>,
                ]),
            <Trans key="joined" id="admin.user.joined">
              Joined {joined}
            </Trans>,
            account.lastSignInAt === undefined ? (
              <Trans key="seen" id="admin.user.never-signed-in">
                Never signed in
              </Trans>
            ) : (
              <Trans key="seen" id="admin.user.last-signed-in">
                Last signed in <RelativeTime value={account.lastSignInAt} />
              </Trans>
            ),
          ]}
        />
      }
    >
      <AccountProfileSection account={account} />
      <AccountAccessLegacy accountId={account.id} />
      <AccountRegistrationSection accountId={account.id} />
      <AccountDangerSection account={account} />
    </AppDetailLayout>
  );
}
