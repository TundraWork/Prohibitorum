import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { accountQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import {
  ObjectHeader,
  type ObjectStatus,
} from "@/components/custom/ObjectHeader";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { AccountDangerSection } from "@/pages/admin/users/AccountDangerSection";
import { AccountEmailsSection } from "@/pages/admin/users/AccountEmailsSection";
import { AccountIdentitiesSection } from "@/pages/admin/users/AccountIdentitiesSection";
import { AccountPasskeysSection } from "@/pages/admin/users/AccountPasskeysSection";
import { AccountProfileSection } from "@/pages/admin/users/AccountProfileSection";
import { AccountRegistrationSection } from "@/pages/admin/users/AccountRegistrationSection";
import { AccountSessionsSection } from "@/pages/admin/users/AccountSessionsSection";
import { AccountTokensSection } from "@/pages/admin/users/AccountTokensSection";
import { Route } from "@/routes/_protected.admin._admin.users_.$id";

/**
 * One account in the management area: who it is at the top, then a column of
 * sections with an outline beside them. The sign-in fact is shown only when
 * the record carries it: the single-account read does not.
 *
 * The profile form comes first, then how the account gets in (its identities,
 * passkeys, sessions and tokens) and the link that sets that up, and last the
 * changes that stop it. The record is read once here
 * and passed down, since the route loader already has it.
 */
export function AdminUser() {
  const { t, i18n } = useLingui();
  const { id } = Route.useParams();
  const accountId = Number(id);
  const { data: account } = useSuspenseQuery(accountQueryOptions(accountId));

  const session = useQuery(sessionQueryOptions());

  const name = account.displayName || account.username;
  useConsoleCrumb(name);
  const joined = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
  }).format(new Date(account.createdAt));
  const role = account.role;
  const known = role === "admin" || role === "user";
  const statuses: ObjectStatus[] = [];
  // State before role, as the list's badges read.
  if (account.disabled) statuses.push("disabled");
  if (role === "admin") statuses.push("admin");
  if (role === "user") statuses.push("user");
  if (!account.disabled) statuses.push("enabled");

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
          dimmed={account.disabled}
          statuses={statuses}
          identifier={{
            value: `@${account.username}`,
            copyValue: account.username,
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
            ...(account.lastSignInAt === undefined
              ? []
              : [
                  <Trans key="seen" id="admin.user.last-signed-in">
                    Last signed in <RelativeTime value={account.lastSignInAt} />
                  </Trans>,
                ]),
          ]}
        />
      }
    >
      <AccountProfileSection account={account} />
      <AccountEmailsSection accountId={account.id} />
      {/* The anchor an old `?tab=access` link lands on: the first of the four. */}
      <div
        id="access"
        className="flex scroll-mt-[calc(var(--app-sticky-offset)+5rem)] flex-col gap-6"
      >
        <AccountIdentitiesSection accountId={account.id} />
        <AccountPasskeysSection accountId={account.id} />
        <AccountSessionsSection accountId={account.id} />
        <AccountTokensSection accountId={account.id} />
      </div>
      <AccountRegistrationSection accountId={account.id} />
      <AccountDangerSection
        account={account}
        isSelf={session.data?.id === account.id}
      />
    </AppDetailLayout>
  );
}
