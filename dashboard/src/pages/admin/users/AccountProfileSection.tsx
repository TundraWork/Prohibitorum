import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckIcon, CircleAlertIcon, UserIcon } from "lucide-react";
import { ApiError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateAccountMutationOptions } from "@/api/mutations";
import { accountUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { CopyValue } from "@/components/custom/CopyValue";
import { Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
import { Separator } from "@/components/ui/separator";
import { applyServerError, ServerFormError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { AccountRoleField } from "@/pages/admin/users/AccountRoleField";

type Account = components["schemas"]["AccountView"];

const attributesInvalid = msg({
  id: "admin.user.attributes.invalid",
  message: "This is not valid JSON. Check the braces, commas and quotes.",
});
const attributesNotObject = msg({
  id: "admin.user.attributes.not_object",
  message: "Attributes must be a JSON object, such as {}.",
});
const adminNeedsEnabling = msg({
  id: "admin.user.role.admin-held",
  message: "Enable the account before making it an admin.",
});
const displayNameRequired = msg({
  id: "admin.user.displayName.required",
  message: "Give the account a display name.",
});

/**
 * Parses the attributes box. An empty box is an empty map rather than an error:
 * `PUT /accounts/{id}` replaces the whole record, so the form always has a map
 * to send, and "no attributes" has to be expressible.
 */
function parseAttributes(
  value: string,
):
  | { ok: true; value: Record<string, unknown> }
  | { ok: false; empty: boolean } {
  const trimmed = value.trim();
  if (trimmed === "") return { ok: true, value: {} };
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { ok: false, empty: false };
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return { ok: false, empty: true };
  }
  return { ok: true, value: parsed as Record<string, unknown> };
}

/** The stored map as the box shows it; an empty map stays an empty box. */
function formatAttributes(attributes?: Record<string, unknown>): string {
  if (!attributes || Object.keys(attributes).length === 0) return "";
  return JSON.stringify(attributes, null, 2);
}

/** The form's values for a stored record, also the baseline after a save. */
function profileValues(account: Account) {
  return {
    displayName: account.displayName,
    email: account.email ?? "",
    role: account.role,
    attributes: formatAttributes(account.attributes),
  };
}

/**
 * The account's name, address, role and attributes, with the OIDC subject
 * applications know it by. One save covers the four fields.
 *
 * The write replaces the record, so the attributes box always submits the full
 * map and the address is sent only when it changed (the server marks any
 * address it is given as unverified). A change of role is the consequential
 * save: the footer says what it gives or takes away, and the write asks for the
 * identity check.
 */
export function AccountProfileSection({ account }: { account: Account }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateAccountMutationOptions(queryClient), true),
  );
  // Named, so the footer's sentences carry it as `{name}` in both catalogs.
  const name = account.displayName || account.username;

  const form = useAppForm({
    defaultValues: profileValues(account),
    onSubmit: async ({ value }) => {
      const attributes = parseAttributes(value.attributes);
      if (!attributes.ok) {
        form.setFieldMeta("attributes", (meta) => ({
          ...meta,
          errorMap: {
            ...meta.errorMap,
            onSubmit: attributes.empty
              ? attributesNotObject
              : attributesInvalid,
          },
        }));
        return;
      }
      const email = value.email.trim();
      const body = accountUpdateBody(account, {
        displayName: value.displayName.trim(),
        role: value.role,
        attributes: attributes.value,
        ...(email === (account.email ?? "") ? {} : { email }),
      });
      try {
        const saved = await update.mutateAsync({
          id: account.id,
          body,
          previousRole: account.role,
        });
        // The stored record is the new baseline, so the section reads as saved.
        form.reset(profileValues(saved));
      } catch (error) {
        if (isCancellation(error)) return;
        if (
          error instanceof ApiError &&
          error.code === "admin_cannot_be_disabled"
        ) {
          // The write carries the stored `disabled`, so the refusal is about
          // the role: say what to do about it where the reader chose it.
          form.setErrorMap({
            onSubmit: {
              fields: { role: new ServerFormError(error, adminNeedsEnabling) },
            },
          });
          return;
        }
        applyServerError(form, error, {
          locations: {
            displayName: "displayName",
            email: "email",
            attributes: "attributes",
          },
          codes: { invalid_role: "role" },
        });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const role = useStore(form.store, (state) => state.values.role);
  const emailUnchanged = useStore(
    form.store,
    (state) => state.values.email.trim() === (account.email ?? ""),
  );
  const roleChanged = role !== account.role;
  const title = t({ id: "admin.user.profile.title", message: "Profile" });

  return (
    <form.AppForm>
      <form.Form
        label={t({ id: "admin.user.profile.form", message: "Account profile" })}
      >
        <Section
          id="profile"
          icon={<UserIcon />}
          title={title}
          description={
            <Trans id="admin.user.profile.description">
              The name, address and role of the account, and the attributes
              applications can read.
            </Trans>
          }
          dirty={dirty}
          footer={
            dirty ? (
              <ConsequenceFooter
                consequence={
                  roleChanged && (role === "admin" || role === "user") ? (
                    role === "admin" ? (
                      <Trans id="admin.user.role.consequence.admin">
                        {name} gets access to the administration pages.
                      </Trans>
                    ) : (
                      <Trans id="admin.user.role.consequence.user">
                        {name} loses access to the administration pages.
                      </Trans>
                    )
                  ) : undefined
                }
                actionLabel={
                  roleChanged && role === "admin" ? (
                    <Trans id="admin.user.role.action.admin">
                      Make admin and save
                    </Trans>
                  ) : roleChanged && role === "user" ? (
                    <Trans id="admin.user.role.action.user">
                      Make user and save
                    </Trans>
                  ) : (
                    <Trans id="admin.user.save">Save changes</Trans>
                  )
                }
                onDiscard={() => form.reset()}
                pending={submitting || update.isPending}
              />
            ) : undefined
          }
        >
          <div className="flex flex-col gap-6">
            <form.FormError />

            <form.AppField
              name="displayName"
              validators={{
                onBlur: ({ value }) =>
                  value.trim() === "" ? displayNameRequired : undefined,
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.user.displayName">Display name</Trans>
                  }
                  autoComplete="off"
                />
              )}
            </form.AppField>

            <form.AppField name="email">
              {(field) => (
                <field.FormField
                  label={<Trans id="admin.user.email">Email</Trans>}
                  description={
                    <>
                      <Trans id="admin.user.email.hint">
                        Optional. Leave it empty to store no address.
                      </Trans>
                      {account.email && emailUnchanged && (
                        <EmailVerification verified={account.emailVerified} />
                      )}
                    </>
                  }
                  type="email"
                  autoComplete="off"
                />
              )}
            </form.AppField>

            <form.AppField name="role">
              {() => (
                <AccountRoleField
                  savedRole={account.role}
                  accountDisabled={account.disabled}
                />
              )}
            </form.AppField>

            <form.AppField
              name="attributes"
              validators={{
                onBlur: ({ value }) => {
                  const parsed = parseAttributes(value);
                  if (parsed.ok) return undefined;
                  return parsed.empty ? attributesNotObject : attributesInvalid;
                },
              }}
            >
              {(field) => (
                <field.TextAreaField
                  label={<Trans id="admin.user.attributes">Attributes</Trans>}
                  description={
                    <Trans id="admin.user.attributes.hint">
                      A JSON object stored with the account. Saving sends
                      whatever is in this box, so anything removed here is
                      removed from the account.
                    </Trans>
                  }
                  rows={8}
                  spellCheck={false}
                  isMonospace
                />
              )}
            </form.AppField>

            <Separator />
            <SettingRow
              descriptionId="admin-user-subject-hint"
              title={<Trans id="admin.user.oidcSubject">OIDC subject</Trans>}
              description={
                <Trans id="admin.user.oidcSubject.hint">
                  Applications identify this account by this value. It never
                  changes.
                </Trans>
              }
            >
              <div className="w-full sm:w-80">
                <CopyValue
                  value={account.oidcSubject}
                  label={
                    <Trans id="admin.user.oidcSubject">OIDC subject</Trans>
                  }
                  labelHidden
                  describedBy="admin-user-subject-hint"
                />
              </div>
            </SettingRow>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}

/** Whether the stored address has been confirmed, in words and an icon. */
function EmailVerification({ verified }: { verified: boolean }) {
  return (
    <span className="mt-1 flex items-center gap-1.5 text-xs">
      {verified ? (
        <>
          <CheckIcon aria-hidden="true" className="size-3.5 text-success" />
          <Trans id="admin.user.email.verified">Verified</Trans>
        </>
      ) : (
        <>
          <CircleAlertIcon
            aria-hidden="true"
            className="size-3.5 text-warning"
          />
          <Trans id="admin.user.email.unverified">Not verified</Trans>
        </>
      )}
    </span>
  );
}
