import { Label, ListBox, Select } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { describeError } from "@/api/errors";
import { readPrincipalSource } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthProjectionMutationOptions } from "@/api/mutations";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * The account facts `Remote-User` may be, and what each one costs.
 *
 * The three differ in what the protected service sees, not in how the console
 * behaves: `sub` is stable and opaque, `username` is readable but an
 * administrator can change it, `verified_email` is readable but becomes
 * unavailable if the address loses verification or is shared with another
 * account — in which case verification returns 403 rather than falling back to
 * a different identifier. Naming them that way is the point of the field: the
 * reader is deciding what their own application keys its users on.
 */
const sources: readonly {
  value: PrincipalSource;
  label: ReturnType<typeof msg>;
}[] = [
  {
    value: "sub",
    label: msg({
      id: "admin.forward-auth-apps.source.sub",
      message: "Account ID",
    }),
  },
  {
    value: "username",
    label: msg({
      id: "admin.forward-auth-apps.source.username",
      message: "Username",
    }),
  },
  {
    value: "verified_email",
    label: msg({
      id: "admin.forward-auth-apps.source.email",
      message: "Verified email",
    }),
  },
];

/** A lookup that cannot come back empty, for the label helper below. */
const sourceLabels = new Map(
  sources.map((source) => [source.value, source.label]),
);

/**
 * The identifier a downstream service sees for a signed-in person.
 *
 * This one field is the most consequential on the page, so it does not go
 * through the usual section form. The write is not a whole-record `PUT` — the
 * projection has its own endpoint and needs no step-up — and the choice must be
 * confirmed before it is sent, which means the select cannot hold the new value
 * as its committed one: it opens a dialog, and the write happens from there, so
 * a reader who cancels leaves the saved value untouched on screen.
 *
 * The `ConfirmDialog` carries the cost of the change rather than a generic
 * warning, because the cost is what is being decided: every protected
 * application starts treating an existing person as a different account, and
 * anything the service already keyed on the old identifier is not migrated.
 */
export function ForwardAuthIdentitySection({ app }: { app: ForwardAuthApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateForwardAuthProjectionMutationOptions(queryClient),
  );
  const [pending, setPending] = useState<PrincipalSource | null>(null);

  // The server answers with the stored source, but a record written before it
  // did reads as unknown; `username` is the server's own default, so that is
  // what the console shows rather than an empty select.
  const saved = readPrincipalSource(app.remoteUserSource) ?? "username";
  const label = (source: PrincipalSource) =>
    i18n._(
      sourceLabels.get(source) ?? {
        id: "admin.forward-auth-apps.source.sub",
        message: "Account ID",
      },
    );

  return (
    <Section
      title={
        <Trans id="admin.forward-auth-apps.identity">Identity projection</Trans>
      }
    >
      <ConsoleCard>
        <div className="flex flex-col gap-1.5">
          <Label>
            <Trans id="admin.forward-auth-apps.identity.source">
              Remote-User
            </Trans>
          </Label>
          <Select
            className="w-full"
            variant="secondary"
            value={saved}
            onChange={(next) => {
              const source = readPrincipalSource(next);
              if (source === undefined || source === saved) return;
              setPending(source);
            }}
          >
            <Select.Trigger>
              <Select.Value />
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox>
                {sources.map((source) => (
                  <ListBox.Item
                    id={source.value}
                    key={source.value}
                    textValue={i18n._(source.label)}
                  >
                    {i18n._(source.label)}
                    <ListBox.ItemIndicator />
                  </ListBox.Item>
                ))}
              </ListBox>
            </Select.Popover>
          </Select>
        </div>

        {update.error !== null && update.error !== undefined && (
          <div className="mt-4">
            <SurfaceAlert status="danger" role="alert">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  {t(describeError(update.error))}
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          </div>
        )}
      </ConsoleCard>

      <ConfirmDialog
        isOpen={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        status="warning"
        title={
          <Trans id="admin.forward-auth-apps.identity.confirm.title">
            Send a different identifier?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.forward-auth-apps.identity.confirm.body">
              The protected application will know this account as its{" "}
              {pending === null ? "" : label(pending)} instead of its{" "}
              {label(saved)}. Anything it has already keyed on the old value is
              not updated, so it will treat the account as a new one.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.forward-auth-apps.identity.confirm.action">
            Change it
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pending === null) return;
          // Settled either way: a refusal has already reported itself under the
          // select, so there is nothing left for the dialog to offer.
          update.mutate(
            { clientId: app.clientId, body: { remoteUserSource: pending } },
            { onSettled: () => setPending(null) },
          );
        }}
      />
    </Section>
  );
}
