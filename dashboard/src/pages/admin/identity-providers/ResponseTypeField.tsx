import { Trans } from "@lingui/react/macro";
import { OptionSelectField } from "@/components/custom/OptionSelectField";
import {
  type ResponseType,
  responseTypes,
} from "@/pages/admin/identity-providers/provider-options";

/**
 * What the provider sends back after sign-in, as a form field.
 *
 * ID token only fails at the provider, not here, when its client does not
 * allow that response, and each console names the switch differently, so the
 * description names the two the reader is most likely to be looking at.
 */
export function ResponseTypeField() {
  return (
    <OptionSelectField<ResponseType>
      label={
        <Trans id="admin.federation.response.label">Sign-in response</Trans>
      }
      options={responseTypes}
      description={
        <Trans id="admin.federation.response.description">
          ID token only works when the provider's client allows
          response_type=id_token. Keycloak calls this Implicit Flow Enabled; the
          Microsoft identity platform lists it as ID tokens under implicit
          grant.
        </Trans>
      }
    />
  );
}
