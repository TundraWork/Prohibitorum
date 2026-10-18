import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Fingerprint,
  LifeBuoy,
  MonitorSmartphone,
  RectangleEllipsis,
  ShieldCheck,
} from "lucide-react";
import type { ReactNode } from "react";
import {
  factorsQueryOptions,
  sessionQueryOptions,
  sessionsQueryOptions,
} from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { AsyncSection, Section } from "@/components/custom/Section";
import { Badge } from "@/components/ui/badge";

/** Codes this few or fewer are worth a mark on the row. */
const lowRecoveryCodes = 3;

/**
 * The account at a glance: how it signs in and where it is signed in. Each
 * row is a link to the page that manages it, with the current value at the end.
 */
export function Console() {
  const { name } = useInstanceBranding();
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  if (session === null) return null;
  const { displayName } = session;
  return (
    <PageBody>
      <PageHeader
        title={<Trans id="console.page.title">Welcome, {displayName}</Trans>}
        description={
          <Trans id="console.page.description">{name} account</Trans>
        }
      />
      <SignInSummary />
      <DevicesSummary />
    </PageBody>
  );
}

const signInTitle = <Trans id="console.signin.title">Sign-in</Trans>;
const devicesTitle = <Trans id="devices.page.title">Devices</Trans>;

function SignInSummary() {
  return (
    <AsyncSection
      resetKey="console-sign-in"
      icon={<ShieldCheck />}
      title={signInTitle}
    >
      <SignInRows />
    </AsyncSection>
  );
}

function SignInRows() {
  const { t } = useLingui();
  const { data } = useSuspenseQuery(factorsQueryOptions());
  const { passkeyCount, passwordSet, totpEnrolled } = data;
  // The plural's placeholder takes its name from this variable.
  const count = data.recoveryCodesRemaining;
  return (
    <Section title={signInTitle} icon={<ShieldCheck />}>
      <ItemList
        label={t({
          id: "console.signin.list",
          message: "Sign-in summary",
        })}
        empty={null}
      >
        <ItemListRow
          key="passkeys"
          icon={<Fingerprint />}
          title={<Trans id="security.section.passkeys">Passkeys</Trans>}
          meta={
            passkeyCount === 0 ? (
              <Trans id="security.passkeys.empty.title">No passkeys yet</Trans>
            ) : (
              <Plural
                id="console.passkeys.count"
                value={passkeyCount}
                one="# passkey"
                other="# passkeys"
              />
            )
          }
          link={securityLink}
        />
        <ItemListRow
          key="password"
          icon={<RectangleEllipsis />}
          title={
            <Trans id="security.section.password">
              Password and authenticator
            </Trans>
          }
          meta={
            passwordSet && totpEnrolled ? (
              <Trans id="console.password.on">On</Trans>
            ) : (
              <Trans id="console.password.off">Not set up</Trans>
            )
          }
          link={securityLink}
        />
        {totpEnrolled && (
          <ItemListRow
            key="recovery-codes"
            icon={<LifeBuoy />}
            title={<Trans id="security.recovery.title">Recovery codes</Trans>}
            badge={
              count === 0 ? (
                <Badge variant="destructive">
                  <Trans id="security.recovery.used_up">All used</Trans>
                </Badge>
              ) : count <= lowRecoveryCodes ? (
                <Badge variant="warning">
                  <Trans id="security.recovery.low">Running low</Trans>
                </Badge>
              ) : undefined
            }
            meta={
              <Plural
                id="security.recovery.remaining"
                value={count}
                one="# code left"
                other="# codes left"
              />
            }
            link={securityLink}
          />
        )}
      </ItemList>
    </Section>
  );
}

const securityLink = ({
  className,
  children,
}: {
  className: string;
  children: ReactNode;
}) => (
  <Link to="/security" className={className}>
    {children}
  </Link>
);

const devicesLink = ({
  className,
  children,
}: {
  className: string;
  children: ReactNode;
}) => (
  <Link to="/devices" className={className}>
    {children}
  </Link>
);

function DevicesSummary() {
  return (
    <AsyncSection
      resetKey="console-devices"
      icon={<MonitorSmartphone />}
      title={devicesTitle}
    >
      <DevicesRow />
    </AsyncSection>
  );
}

function DevicesRow() {
  const { t } = useLingui();
  const { data } = useSuspenseQuery(quietQuery(sessionsQueryOptions()));
  const count = data.length;
  return (
    <Section title={devicesTitle} icon={<MonitorSmartphone />}>
      <ItemList
        label={t({
          id: "console.devices.list",
          message: "Devices summary",
        })}
        empty={null}
      >
        <ItemListRow
          icon={<MonitorSmartphone />}
          title={
            <Trans id="console.devices.row">Where you are signed in</Trans>
          }
          meta={
            <Plural
              id="console.devices.count"
              value={count}
              one="# signed-in device"
              other="# signed-in devices"
            />
          }
          link={devicesLink}
        />
      </ItemList>
    </Section>
  );
}
