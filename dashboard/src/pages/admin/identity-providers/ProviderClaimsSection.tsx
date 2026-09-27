import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readOidcProviderConfig,
} from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import type { ProviderWriteBody } from "@/api/raw-admin-paths";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { TagListField } from "@/components/custom/TagListField";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  ClaimMappingRow,
  ClaimMappingTable,
} from "@/pages/admin/identity-providers/ClaimMappingTable";
import {
  claimNameProblem,
  domainProblem,
  subjectChanged,
  tagDraftProblem,
  tagListValue,
} from "@/pages/admin/identity-providers/provider-validation";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * Which people a provider may bring in, and how their claims become an
 * account's own fields.
 *
 * The allowed domains are the gate: an empty list accepts every address the
 * provider vouches for, which is why the field says so rather than leaving an
 * empty list to be read as "none". The claim names decide what fills in the
 * account; a wrong one produces an account with a blank name rather than an
 * error, so each has its own row with the server's default as its example.
 *
 * The subject claim is what a returning person is matched on, together with
 * the issuer. Changing it while accounts are linked leaves every link keyed on a
 * value that no longer arrives, so the save is confirmed: the button turns to
 * its warning tone while the claim differs from the saved one, and submitting
 * opens the dialog; cancelling leaves the change in the form, unsent. A
 * provider nobody has linked through saves straight away.
 */
export function ProviderClaimsSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateIdentityProviderMutationOptions(queryClient),
  );
  // The checked request the dialog is confirming, and the claim it switches
  // to. It outlives the dialog's closing so the text does not change while the
  // dialog fades out.
  const [pending, setPending] = useState<{
    body: ProviderWriteBody;
    subject: string;
  } | null>(null);
  const [confirming, setConfirming] = useState(false);

  const saved =
    readOidcProviderConfig(provider.config) ?? defaultOidcProviderConfig();
  const linked = provider.linkedAccountCount ?? 0;

  const form = useAppForm({
    defaultValues: {
      allowedDomains: tagListValue(saved.allowedDomains),
      requireVerifiedEmail: saved.requireVerifiedEmail,
      usernameClaim: saved.usernameClaim,
      displayNameClaim: saved.displayNameClaim,
      emailClaim: saved.emailClaim,
      pictureClaim: saved.pictureClaim,
      subjectClaim: saved.subjectClaim,
    },
    onSubmit: async ({ value }) => {
      const body = identityProviderUpdateBody(provider, {
        config: {
          ...saved,
          allowedDomains: value.allowedDomains.tags,
          requireVerifiedEmail: value.requireVerifiedEmail,
          usernameClaim: value.usernameClaim,
          displayNameClaim: value.displayNameClaim,
          emailClaim: value.emailClaim,
          pictureClaim: value.pictureClaim,
          subjectClaim: value.subjectClaim,
        },
      });
      if (
        linked > 0 &&
        subjectChanged(saved.subjectClaim, value.subjectClaim)
      ) {
        setPending({ body, subject: value.subjectClaim });
        setConfirming(true);
        return;
      }
      await save(body);
    },
  });

  async function save(body: ProviderWriteBody) {
    try {
      await update.mutateAsync({ slug: provider.slug, body });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, {
        codes: { bad_request: "subjectClaim" },
        locations: {},
      });
    }
  }

  const warns = useStore(
    form.store,
    (state) =>
      linked > 0 &&
      subjectChanged(saved.subjectClaim, state.values.subjectClaim),
  );

  const title = t({
    id: "admin.federation.claims.title",
    message: "Accounts and claims",
  });
  const claimValidator = {
    onSubmit: ({ value }: { value: string }) => claimNameProblem(value),
  };

  // Named, so the confirmation carries them as `{name}` placeholders the
  // catalogs share.
  const count = linked;
  const name = provider.displayName || provider.slug;
  const current = saved.subjectClaim;
  const next = pending?.subject ?? "";

  return (
    <Section title={title}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form label={title}>
            <form.FormError />

            <form.AppField
              name="allowedDomains"
              validators={{ onSubmit: ({ value }) => tagDraftProblem(value) }}
            >
              {() => (
                <TagListField
                  label={t({
                    id: "admin.federation.claims.domains",
                    message: "Allowed email domains",
                  })}
                  description={
                    <Trans id="admin.federation.claims.domains.hint">
                      Leave it empty to accept every domain.
                    </Trans>
                  }
                  placeholder="example.com"
                  addLabel={<Trans id="form.tags.add">Add</Trans>}
                  check={domainProblem}
                />
              )}
            </form.AppField>

            <form.AppField name="requireVerifiedEmail">
              {(field) => (
                <field.SwitchField
                  label={
                    <Trans id="admin.federation.claims.verified">
                      Require a verified email address
                    </Trans>
                  }
                  description={
                    <Trans id="admin.federation.claims.verified.hint">
                      Only accepts addresses the provider marks as verified.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <ClaimMappingTable>
              <form.AppField name="usernameClaim" validators={claimValidator}>
                {() => (
                  <ClaimMappingRow
                    name={t({
                      id: "admin.federation.claims.username",
                      message: "Username",
                    })}
                    placeholder="preferred_username"
                  />
                )}
              </form.AppField>
              <form.AppField
                name="displayNameClaim"
                validators={claimValidator}
              >
                {() => (
                  <ClaimMappingRow
                    name={t({
                      id: "admin.federation.claims.display-name",
                      message: "Display name",
                    })}
                    placeholder="name"
                  />
                )}
              </form.AppField>
              <form.AppField name="emailClaim" validators={claimValidator}>
                {() => (
                  <ClaimMappingRow
                    name={t({
                      id: "admin.federation.claims.email",
                      message: "Email",
                    })}
                    placeholder="email"
                  />
                )}
              </form.AppField>
              <form.AppField name="pictureClaim" validators={claimValidator}>
                {() => (
                  <ClaimMappingRow
                    name={t({
                      id: "admin.federation.claims.picture",
                      message: "Picture",
                    })}
                    placeholder="picture"
                  />
                )}
              </form.AppField>
              <form.AppField name="subjectClaim" validators={claimValidator}>
                {() => (
                  <ClaimMappingRow
                    name={t({
                      id: "admin.federation.claims.subject",
                      message: "Subject",
                    })}
                    hint={
                      <Trans id="admin.federation.claims.subject.hint">
                        Accounts are linked by this value.
                      </Trans>
                    }
                    placeholder="sub"
                  />
                )}
              </form.AppField>
            </ClaimMappingTable>

            <form.SubmitButton tone={warns ? "warning" : "default"}>
              <Trans id="admin.federation.claims.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setConfirming(false);
        }}
        status="warning"
        title={
          <Trans id="admin.federation.claims.subject.confirm.title">
            Change the subject claim?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.federation.claims.subject.confirm.body">
              <Plural value={count} one="# account is" other="# accounts are" />{" "}
              linked to {name} by their {current} claim. After switching to{" "}
              {next}, those links no longer match, and each person is treated as
              a new identity the next time they sign in.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.federation.claims.subject.confirm.action">
            Change and save
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pending === null) return;
          // Closed once the write settles either way: a refusal is reported
          // on the form, where the reader can act on it.
          void save(pending.body).finally(() => setConfirming(false));
        }}
      />
    </Section>
  );
}
