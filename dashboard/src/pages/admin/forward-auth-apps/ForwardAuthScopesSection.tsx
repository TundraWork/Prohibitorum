import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthAppMutationOptions } from "@/api/mutations";
import type { UpdateForwardAuthAppRequest } from "@/api/raw-admin-paths";
import { forwardAuthAppUpdateBody } from "@/api/update-bodies";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  removedScopeNames,
  scopeListProblem,
} from "@/pages/admin/forward-auth-apps/forward-auth-validation";
import {
  type ScopeRow,
  ScopeTableField,
  scopeRowProblem,
} from "@/pages/admin/forward-auth-apps/ScopeTableField";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/** The anchor the `Remote-Scopes` header row links to. */
export const tokenScopesAnchor = "token-scopes";

/**
 * The scopes a personal access token may be granted for this application, and
 * which the service then receives in `Remote-Scopes`.
 *
 * Its own section with its own save, although the scopes are part of the same
 * record as the name and the host: a save here sends the general fields as they
 * are stored, so an unsaved edit in one block never goes out with the other.
 * The body still comes from `forwardAuthAppUpdateBody`, which is where the
 * whole-record merge lives.
 *
 * Removing a saved scope is confirmed when the table is saved, because the
 * removal does not reach the tokens that already hold it: the server checks a
 * grant when the token is issued, so a token granted the scope keeps sending it
 * until it is revoked, and only new tokens lose the choice. The save button
 * turns to its warning tone as soon as a saved scope is missing from the table,
 * and the dialog names the scopes. Adding a scope or rewording a description
 * has no such cost and saves straight away.
 */
export function ForwardAuthScopesSection({ app }: { app: ForwardAuthApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateForwardAuthAppMutationOptions(queryClient));
  // The request the dialog is confirming, checked and ready to send, with the
  // names it removes. It outlives the dialog's closing so the text does not
  // change while the dialog fades out.
  const [pending, setPending] = useState<{
    body: UpdateForwardAuthAppRequest;
    removed: string[];
  } | null>(null);
  const [confirming, setConfirming] = useState(false);

  const saved = app.scopes ?? [];

  const form = useAppForm({
    defaultValues: {
      scopes: saved.map(
        (scope): ScopeRow => ({
          name: scope.name,
          description: scope.description ?? "",
        }),
      ),
    },
    onSubmit: async ({ value }) => {
      const problem = scopeListProblem(value.scopes);
      if (problem !== undefined) {
        form.setFieldMeta("scopes", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: [scopeRowProblem(problem)] },
        }));
        return;
      }

      const body = forwardAuthAppUpdateBody(app, {
        // An empty description is omitted rather than sent empty: the server
        // reports one back only when it is non-empty, so sending `""` would
        // make the next read disagree with what was saved.
        scopes: value.scopes.map((scope) => ({
          name: scope.name,
          ...(scope.description.trim() === ""
            ? {}
            : { description: scope.description.trim() }),
        })),
      });
      const removed = removedScopeNames(saved, value.scopes);
      if (removed.length > 0) {
        setPending({ body, removed });
        setConfirming(true);
        return;
      }
      await save(body);
    },
  });

  async function save(body: UpdateForwardAuthAppRequest) {
    try {
      await update.mutateAsync({ clientId: app.clientId, body });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, {
        codes: { bad_request: "scopes" },
        locations: {},
      });
    }
  }

  const removesScopes = useStore(
    form.store,
    (state) => removedScopeNames(saved, state.values.scopes).length > 0,
  );

  const title = t({
    id: "admin.forward-auth-apps.scopes",
    message: "Token scopes",
  });
  const removedNames =
    pending === null
      ? ""
      : new Intl.ListFormat(i18n.locale, { type: "conjunction" }).format(
          pending.removed,
        );
  const removedCount = pending?.removed.length ?? 0;

  return (
    <Section id={tokenScopesAnchor} title={title}>
      <ConsoleCard wide>
        <form.AppForm>
          <form.Form label={title}>
            <form.FormError />

            <form.AppField name="scopes">
              {() => <ScopeTableField isLabelHidden />}
            </form.AppField>

            <form.SubmitButton tone={removesScopes ? "warning" : "default"}>
              <Trans id="admin.forward-auth-apps.save">Save</Trans>
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
          <Plural
            id="admin.forward-auth-apps.scopes.remove.title"
            value={removedCount}
            one="Remove # scope?"
            other="Remove # scopes?"
          />
        }
        body={
          <p>
            <Plural
              id="admin.forward-auth-apps.scopes.remove.body"
              value={removedCount}
              one={`Tokens already granted ${removedNames} keep sending it until they are revoked. New tokens can no longer choose it.`}
              other={`Tokens already granted ${removedNames} keep sending them until they are revoked. New tokens can no longer choose them.`}
            />
          </p>
        }
        confirmLabel={
          <Trans id="admin.forward-auth-apps.scopes.remove.action">
            Remove and save
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
