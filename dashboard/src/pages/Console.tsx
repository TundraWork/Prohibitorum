import { Plural, Trans } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Fingerprint, Laptop } from "lucide-react";
import type { ReactNode } from "react";
import {
  factorsQueryOptions,
  sessionQueryOptions,
  sessionsQueryOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { AsyncSection, Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
import { cn } from "@/lib/utils";

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
const devicesTitle = <Trans id="console.devices.title">Devices</Trans>;

/** The row's value as a link to the page behind it. */
function ValueLink({
  to,
  tone,
  children,
}: {
  to: "/security" | "/devices";
  tone?: "warning" | "destructive";
  children: ReactNode;
}) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className={cn(
        "max-sm:-ms-2.5 justify-self-start sm:justify-self-end",
        tone === "warning" && "text-warning-foreground",
        tone === "destructive" && "text-destructive-foreground",
      )}
      render={<Link to={to} data-tone={tone} />}
    >
      {children}
      <ChevronRight aria-hidden="true" />
    </Button>
  );
}

function SignInSummary() {
  return (
    <AsyncSection
      resetKey="console-sign-in"
      icon={<Fingerprint />}
      title={signInTitle}
    >
      <SignInRows />
    </AsyncSection>
  );
}

function SignInRows() {
  const { data } = useSuspenseQuery(factorsQueryOptions());
  const { passkeyCount, passwordSet, totpEnrolled } = data;
  // The plural's placeholder takes its name from this variable.
  const count = data.recoveryCodesRemaining;
  const tone =
    count === 0
      ? "destructive"
      : count <= lowRecoveryCodes
        ? "warning"
        : undefined;
  return (
    <Section title={signInTitle} icon={<Fingerprint />}>
      <SettingRow title={<Trans id="console.passkeys.title">Passkeys</Trans>}>
        <ValueLink to="/security">
          {passkeyCount === 0 ? (
            <Trans id="console.passkeys.none">No passkeys yet</Trans>
          ) : (
            <Plural
              id="console.passkeys.count"
              value={passkeyCount}
              one="# passkey"
              other="# passkeys"
            />
          )}
        </ValueLink>
      </SettingRow>
      <SettingRow
        title={
          <Trans id="console.password.title">Password and authenticator</Trans>
        }
      >
        <ValueLink to="/security">
          {passwordSet && totpEnrolled ? (
            <Trans id="console.password.on">On</Trans>
          ) : (
            <Trans id="console.password.off">Not set up</Trans>
          )}
        </ValueLink>
      </SettingRow>
      {totpEnrolled && (
        <SettingRow
          title={<Trans id="console.recovery.title">Recovery codes</Trans>}
          description={
            tone === "destructive" ? (
              <Trans id="console.recovery.used_up">All used</Trans>
            ) : tone === "warning" ? (
              <Trans id="console.recovery.low">Running low</Trans>
            ) : undefined
          }
        >
          <ValueLink to="/security" tone={tone}>
            <Trans id="console.recovery.remaining">{count} left</Trans>
          </ValueLink>
        </SettingRow>
      )}
    </Section>
  );
}

function DevicesSummary() {
  return (
    <AsyncSection
      resetKey="console-devices"
      icon={<Laptop />}
      title={devicesTitle}
    >
      <DevicesRow />
    </AsyncSection>
  );
}

function DevicesRow() {
  const { data } = useSuspenseQuery(sessionsQueryOptions());
  const count = data.length;
  return (
    <Section title={devicesTitle} icon={<Laptop />}>
      <SettingRow
        title={<Trans id="console.devices.row">Where you are signed in</Trans>}
      >
        <ValueLink to="/devices">
          <Plural
            id="console.devices.count"
            value={count}
            one="# signed-in device"
            other="# signed-in devices"
          />
        </ValueLink>
      </SettingRow>
    </Section>
  );
}
