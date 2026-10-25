import { Trans } from "@lingui/react/macro";
import { ProviderChoiceField } from "@/pages/admin/identity-providers/ProviderChoiceField";
import {
  type ResponseType,
  responseTypes,
} from "@/pages/admin/identity-providers/provider-options";

/**
 * What the provider sends back after sign-in, as a form field.
 *
 * The implicit responses fail at the provider, not here, when its client does
 * not allow that response, and each console names the switch differently, so
 * the description names the one the reader is most likely to be looking at.
 */
export function ResponseTypeField() {
  return (
    <ProviderChoiceField<ResponseType>
      label={
        <Trans id="admin.federation.response.label">Sign-in response</Trans>
      }
      options={responseTypes}
      description={
        <Trans id="admin.federation.response.description">
          ID token only requires the provider's client to allow
          response_type=id_token, and ID token + access token requires
          response_type=id_token token. In Keycloak this is Implicit Flow
          Enabled; in the Microsoft identity platform, it is ID tokens and
          Access tokens under implicit grant.
        </Trans>
      }
    />
  );
}
