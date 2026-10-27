import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { UserRoundCheckIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readOidcProviderConfig,
} from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import { latestConnectionTestQueryOptions } from "@/api/queries";
import type { ProviderWriteBody } from "@/api/raw-admin-paths";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
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

/** The stored configuration, which the other sections leave alone. */
function savedConfig(provider: Provider) {
  return readOidcProviderConfig(provider.config) ?? defaultOidcProviderConfig();
}

/** The form's values for a stored record, also the baseline after a save. */
function claimValues(provider: Provider) {
  const saved = savedConfig(provider);
  return {
    allowedDomains: tagListValue(saved.allowedDomains),
    requireVerifiedEmail: saved.requireVerifiedEmail,
    usernameClaim: saved.usernameClaim,
    displayNameClaim: saved.displayNameClaim,
    emailClaim: saved.emailClaim,
    pictureClaim: saved.pictureClaim,
    subjectClaim: saved.subjectClaim,
  };
}

/**
 * Which people a provider may bring in, and how their claims become an
 * account's own fields.
 *
 * The allowed domains are the gate: an empty list accepts every address the
 * provider vouches for, which is why the field says so rather than leaving an
 * empty list to be read as "none". The claim names decide what fills in the
 * account; a wrong one produces an account with a blank name rather than an
 * error, so each has its own row with the server's default as its example, and
 * beside it what the latest connection test read for that field, so a name can
 * be checked against a real answer.
 *
 * The subject claim is what a returning person is matched on, together with
 * the issuer. Changing it while accounts are linked leaves every link keyed on
 * a value that no longer arrives, so the save is confirmed: while the claim
 * differs from the saved one the footer states what will happen and names the
 * action, and submitting opens the dialog with the checked request held beside
 * the form. Cancelling leaves the edit in the form, unsent. A provider nobody
 * has linked through saves straight away.
 *
 * A failed write is reported once, on the section, and never as a toast too.
 */
export function ProviderClaimsSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateIdentityProviderMutationOptions(queryClient), true),
  );
  // Only decorates the table: a summary that cannot be read leaves the column
  // out instead of failing the section.
  const lastTest = useQuery(latestConnectionTestQueryOptions(provider.slug));
  // The checked request the dialog is confirming, and the claim it switches
  // to. It outlives the dialog's closing so the text does not change while the
  // dialog fades out.
  const [pending, setPending] = useState<{
    body: ProviderWriteBody;
    subject: string;
  } | null>(null);
  const [confirming, setConfirming] = useState(false);

  const saved = savedConfig(provider);
  const linked = provider.linkedAccountCount ?? 0;

  const form = useAppForm({
    defaultValues: claimValues(provider),
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
      const result = await update.mutateAsync({ slug: provider.slug, body });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset(claimValues(result));
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, {
        codes: { bad_request: "subjectClaim" },
        locations: {},
      });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const subject = useStore(form.store, (state) => state.values.subjectClaim);
  const warns = linked > 0 && subjectChanged(saved.subjectClaim, subject);

  const title = t({
    id: "admin.federation.claims.title",
    message: "Accounts and claims",
  });
  const claimValidator = {
    onSubmit: ({ value }: { value: string }) => claimNameProblem(value),
  };

  // Named, so the consequence carries them as `{name}` placeholders the
  // catalogs share.
  const count = linked;
  // Joined with no-break spaces so a provider name is never split across lines
  // where a short fragment of it would be left on a line of its own.
  const name = (provider.displayName || provider.slug).replace(/ /g, "\u00a0");
  const current = saved.subjectClaim;

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="claims"
            icon={<UserRoundCheckIcon />}
            title={title}
            description={
              <Trans id="admin.federation.claims.description">
                Control who can use this provider, and which provider claims
                fill in account fields.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    warns ? (
                      <SubjectConsequence
                        count={count}
                        name={name}
                        current={current}
                        next={subject}
                      />
                    ) : undefined
                  }
                  actionLabel={
                    warns ? (
                      <Trans id="admin.federation.claims.subject.action">
                        Change the subject claim and save
                      </Trans>
                    ) : (
                      <Trans id="admin.federation.claims.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  // The confirmation carries the spinner when it sends the save.
                  pending={submitting}
                  disabled={update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
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
                        Only email addresses on these domains are accepted.
                        Leave empty to accept all domains.
                      </Trans>
                    }
                    emptyText={t({
                      id: "admin.federation.claims.domains.empty",
                      message: "Every domain is accepted.",
                    })}
                    placeholder="example.com"
                    addLabel={t({
                      id: "form.tags.add-domain",
                      message: "Add domain",
                    })}
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

              <ClaimMappingTable lastTest={lastTest.data?.latest}>
                <form.AppField name="usernameClaim" validators={claimValidator}>
                  {() => (
                    <ClaimMappingRow
                      name={t({
                        id: "admin.federation.claims.username",
                        message: "Username",
                      })}
                      testField="username"
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
                      testField="displayName"
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
                      testField="email"
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
                      testField="picture"
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
                          Accounts are linked by the value of this claim.
                        </Trans>
                      }
                      testField="subject"
                      placeholder="sub"
                    />
                  )}
                </form.AppField>
              </ClaimMappingTable>
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setConfirming(false);
        }}
        status="warning"
        title={
          <Trans id="admin.federation.claims.subject.confirm.title">
            Change the subject claim for {name}?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.federation.claims.subject.confirm.short">
              <Plural
                value={count}
                one="# linked account"
                other="# linked accounts"
              />{" "}
              will no longer match and will be treated as a new identity at the
              next sign-in.
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
    </>
  );
}

/** What switching the subject claim does to the accounts linked through it. */
function SubjectConsequence({
  count,
  name,
  current,
  next,
}: {
  count: number;
  name: string;
  current: string;
  next: string;
}) {
  return (
    <Trans id="admin.federation.claims.subject.confirm.body">
      <Plural value={count} one="# account is" other="# accounts are" /> linked
      to {name} by the <code className="font-mono text-xs">{current}</code>{" "}
      claim. After the change to{" "}
      <code className="font-mono text-xs">{next}</code>, these links no longer
      match, and each person is treated as a new identity at their next sign-in.
    </Trans>
  );
}
