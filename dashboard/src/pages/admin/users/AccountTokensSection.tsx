import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, KeyRound, Ticket } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { revokeAccountTokenMutationOptions } from "@/api/mutations";
import { accountTokensQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { useInlineFailure } from "@/components/custom/InlineFailure";
import { ItemListRow } from "@/components/custom/ItemList";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  TokenAccessBadge,
  TokenAccessSummary,
  TokenExpiry,
  TokenLastUsed,
} from "@/components/custom/TokenAccess";
import { AccessSection } from "@/pages/admin/users/AccessSection";

type Token = components["schemas"]["PersonalAccessTokenView"];

/**
 * The access tokens an account has made, each revocable by an administrator.
 * A revoked token stops working at once and cannot be restored; the token
 * itself is never shown here, only its hint.
 */
export function AccountTokensSection({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const tokens = useQuery(quietQuery(accountTokensQueryOptions(accountId)));
  const revoke = useMutation(
    withInlineReport(revokeAccountTokenMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const [target, setTarget] = useState<Token | null>(null);

  return (
    <>
      <AccessSection
        id="tokens"
        icon={<KeyRound />}
        title={<Trans id="admin.user.tokens.title">Access tokens</Trans>}
        description={
          <Trans id="admin.user.tokens.description">
            Keys for scripts and tools that act as them. Revoking one stops it
            working straight away.
          </Trans>
        }
        label={t({ id: "admin.user.tokens.table", message: "Access tokens" })}
        query={tokens}
        writeError={failure.error}
        empty={
          <TableEmptyState
            icon={<Ticket size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={<Trans id="admin.user.tokens.empty">No access tokens</Trans>}
          />
        }
      >
        {(tokens.data?.items ?? []).map((token) => {
          const name = token.name;
          return (
            <ItemListRow
              key={token.id}
              icon={<Ticket aria-hidden="true" />}
              title={name}
              badges={<TokenAccessBadge token={token} />}
              meta={<TokenExpiry token={token} />}
              details={[
                <span key="hint" className="font-mono">
                  {token.tokenHint}
                </span>,
                <TokenAccessSummary key="access" token={token} />,
                <TokenLastUsed key="used" token={token} />,
              ]}
              actions={
                <Button
                  aria-label={t({
                    id: "admin.user.tokens.revoke.named",
                    message: `Revoke ${name}`,
                  })}
                  onClick={() => {
                    failure.clear();
                    setTarget(token);
                  }}
                  variant="destructive-outline"
                  size="icon-sm"
                >
                  <Ban aria-hidden="true" />
                </Button>
              }
            />
          );
        })}
      </AccessSection>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && !revoke.isPending && setTarget(null)}
        status="danger"
        title={
          <Trans id="admin.user.tokens.revoke.title">Revoke this token?</Trans>
        }
        body={
          <p>
            <Trans id="admin.user.tokens.revoke.body">
              Anything using this token stops working immediately. This cannot
              be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.tokens.revoke.action">Revoke token</Trans>
        }
        isPending={revoke.isPending}
        onConfirm={() => {
          if (target === null) return;
          revoke.mutate(
            { accountId, id: target.id },
            { onError: failure.report, onSettled: () => setTarget(null) },
          );
        }}
      />
    </>
  );
}
