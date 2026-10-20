import { Trans, useLingui } from "@lingui/react/macro";
import { RefreshCwIcon } from "lucide-react";
import type {
  EffectiveConfigField,
  EffectiveConfigView,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { CopyableValue } from "@/components/custom/CopyableValue";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  configSourceLabel,
  effectiveConfigFields,
} from "@/pages/admin/identity-providers/provider-options";
import { SourcedList } from "@/pages/admin/identity-providers/SourcedList";

/**
 * What the provider's configuration resolved to: each value with where it came
 * from, and the address a connection test comes back to. The read itself is the
 * section's, so a failed read is reported even when this dialog is closed.
 */
export function EffectiveConfigDialog({
  open,
  onClose,
  fetched,
  pending,
  onReread,
}: {
  open: boolean;
  onClose: () => void;
  fetched: EffectiveConfigView | null;
  /** A read is in flight. */
  pending: boolean;
  onReread: () => void;
}) {
  const { t } = useLingui();
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogPopup className="sm:max-w-2xl" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>
            <Trans id="admin.federation.diagnostics.effective">
              Effective configuration
            </Trans>
          </DialogTitle>
          {fetched !== null && (
            <DialogDescription>
              <Trans id="admin.federation.diagnostics.fetched">
                Read <RelativeTime value={fetched.fetchedAt} />
              </Trans>
            </DialogDescription>
          )}
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-5">
          {fetched !== null && (
            <>
              <EffectiveConfigTable fields={fetched.fields} />
              <div className="flex min-w-0 flex-col gap-1">
                <div className="font-medium text-sm">
                  <Trans id="admin.federation.diagnostics.callback">
                    Test callback address
                  </Trans>
                </div>
                <CopyableValue
                  wrap
                  className="text-[13px]"
                  value={fetched.callbackUrl}
                  label={t({
                    id: "admin.federation.diagnostics.callback.copy",
                    message: "Copy the test callback address",
                  })}
                />
                <p className="text-muted-foreground text-caption">
                  <Trans id="admin.federation.diagnostics.callback.hint">
                    Register this address with the provider before running a
                    connection test.
                  </Trans>
                </p>
              </div>
            </>
          )}
        </DialogPanel>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            <Trans id="admin.federation.diagnostics.close">Close</Trans>
          </Button>
          <Button variant="outline" pending={pending} onClick={onReread}>
            <RefreshCwIcon aria-hidden="true" />
            <Trans id="admin.federation.diagnostics.reread">Read again</Trans>
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

/** The resolved values, each with where it came from. */
function EffectiveConfigTable({
  fields,
}: {
  fields: Record<string, EffectiveConfigField>;
}) {
  const { i18n } = useLingui();
  const rows = effectiveConfigFields.flatMap(({ key, label }) => {
    const field = fields[key];
    if (field === undefined) return [];
    const source = configSourceLabel(field.source);
    return [
      {
        key,
        label: i18n._(label),
        value: <EffectiveValue field={key} value={field.value} />,
        // A value that is not used has no origin worth a badge.
        source:
          source === undefined || valueKind(key, field.value) === "unused"
            ? undefined
            : i18n._(source),
        // The one value an operator changed is told apart from the rest.
        changed: field.source === "override",
      },
    ];
  });
  return <SourcedList rows={rows} />;
}

type ValueKind = "jwks-none" | "scopes-none" | "unused" | "value";

/**
 * The JWKS sentinel and an empty scope list are configured values with a
 * meaning of their own; reading them as "not used" would say the opposite.
 */
function valueKind(field: string, value: string | string[] | null): ValueKind {
  if (field === "jwksEndpoint" && value === "none") return "jwks-none";
  if (field === "scopes" && Array.isArray(value) && value.length === 0) {
    return "scopes-none";
  }
  const empty =
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0);
  return empty ? "unused" : "value";
}

export function EffectiveValue({
  field,
  value,
}: {
  field: string;
  value: string | string[] | null;
}) {
  const kind = valueKind(field, value);
  if (kind === "jwks-none") {
    return (
      <span className="text-muted-foreground text-sm">
        <Trans id="admin.federation.diagnostics.jwks-none">
          Not used — the ID token is skipped
        </Trans>
      </span>
    );
  }
  if (kind === "scopes-none") {
    return (
      <span className="text-muted-foreground text-sm">
        <Trans id="admin.federation.diagnostics.scopes-none">None sent</Trans>
      </span>
    );
  }
  if (kind === "unused" || value === null) {
    return (
      <span className="text-muted-foreground text-sm">
        <Trans id="admin.federation.diagnostics.unused">Not used</Trans>
      </span>
    );
  }
  if (Array.isArray(value)) {
    return (
      <span className="flex flex-wrap gap-1.5">
        {value.map((item) => (
          <Badge key={item} variant="secondary" className="font-mono">
            {item}
          </Badge>
        ))}
      </span>
    );
  }
  return <span className="wrap-anywhere font-mono text-[13px]">{value}</span>;
}
