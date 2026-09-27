import { Description, Label } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import type { ReactNode } from "react";
import type { ProviderMode } from "@/api/raw-admin-paths";
import { OptionSelectField } from "@/components/custom/OptionSelectField";
import {
  providerModeLabel,
  providerModes,
} from "@/pages/admin/identity-providers/provider-options";

/**
 * What a provider does with someone it has not seen, as a form field.
 *
 * Each mode carries its consequence under its name, since the three differ in
 * who ends up with an account, not in anything the reader can see on this page.
 *
 * VRChat links existing accounts and cannot create any, so for it the mode is
 * a fact rather than a choice: the name and why, with no select. A select that
 * ignored what was picked would be worse than one that says it is fixed.
 */
export function ProvisioningModeField({ isFixed }: { isFixed: boolean }) {
  const label = <Trans id="admin.federation.general.mode">Provisioning</Trans>;
  if (isFixed) return <FixedMode label={label} />;
  return (
    <OptionSelectField<ProviderMode> label={label} options={providerModes} />
  );
}

function FixedMode({ label }: { label: ReactNode }) {
  const { i18n } = useLingui();
  return (
    <div className="flex flex-col gap-1">
      <Label>{label}</Label>
      <p className="text-sm text-foreground">
        {i18n._(providerModeLabel("link_only"))}
      </p>
      <Description>
        <Trans id="admin.federation.general.mode.vrchat">
          VRChat accounts can only be linked, never created.
        </Trans>
      </Description>
    </div>
  );
}
