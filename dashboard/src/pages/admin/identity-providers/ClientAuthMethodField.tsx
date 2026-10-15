import { Trans } from "@lingui/react/macro";
import { OptionSelectField } from "@/components/custom/OptionSelectField";
import {
  type ClientAuthMethod,
  clientAuthMethods,
} from "@/pages/admin/identity-providers/provider-options";

/**
 * How this instance authenticates to the provider's token endpoint, as a form
 * field, with what each method sends under its name.
 *
 * "Use what discovery reports" has nothing to report when the endpoints are
 * entered by hand, and the server refuses it there, so it is disabled in that
 * mode. A saved value of it is not changed for the reader; the submit check
 * asks them to pick one instead. The client secret a method sends is entered
 * in the field the connection form draws beside this one, not here.
 */
export function ClientAuthMethodField({
  isDiscoveryUnavailable = false,
}: {
  isDiscoveryUnavailable?: boolean;
}) {
  return (
    <OptionSelectField<ClientAuthMethod>
      label={
        <Trans id="admin.federation.new.auth-method">
          Client authentication
        </Trans>
      }
      options={clientAuthMethods}
      disabledKeys={isDiscoveryUnavailable ? ["discovery"] : undefined}
    />
  );
}
