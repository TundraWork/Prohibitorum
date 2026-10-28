import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  CircleCheckIcon,
  CircleXIcon,
  ExternalLinkIcon,
  FileSearchIcon,
  PlugZapIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useState } from "react";
import {
  refreshEffectiveConfigMutationOptions,
  startDiagnosticMutationOptions,
} from "@/api/mutations";
import { latestConnectionTestQueryOptions } from "@/api/queries";
import type {
  EffectiveConfigView,
  LatestConnectionTestView,
} from "@/api/raw-admin-paths";
import { usePageRestore } from "@/app/use-page-restore";
import { Button } from "@/components/custom/Button";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Section } from "@/components/custom/Section";
import { Badge } from "@/components/ui/badge";
import { ConnectionTestDialog } from "@/pages/admin/identity-providers/ConnectionTestDialog";
import { EffectiveConfigDialog } from "@/pages/admin/identity-providers/EffectiveConfigDialog";

export { runIdPattern } from "@/pages/admin/identity-providers/run-id";

/**
 * Whether the upstream actually works, before anyone depends on it.
 *
 * Two actions, one row each: the effective configuration answers "what did we
 * resolve, and where from", and the connection test answers "does a sign-in go
 * through". What each one finds opens in a dialog rather than growing under
 * the row, so the section keeps the same short shape before and after.
 *
 * Neither is fetched on first paint: both reach the upstream, and the server
 * rate-limits them (twenty a minute for the configuration, six for a test), so
 * both are reads the reader asks for. The test leaves the page: the browser is
 * sent to the provider's authorization endpoint and comes back with
 * `?test=<id>`, which is the only way this page learns that a run exists, and
 * the result dialog opens for that run. The requests are made here, not in the
 * dialogs, so a failure is reported once even after its dialog has closed, in
 * the console's error toast and nowhere else.
 */
export function ProviderDiagnosticsSection({
  slug,
  runId,
  onRunIdChange,
}: {
  slug: string;
  /** The run named by `?test=`, when the page was opened with one. */
  runId: string | undefined;
  /** Changes `?test=`; `undefined` removes it. */
  onRunIdChange: (runId: string | undefined) => void;
}) {
  const { t } = useLingui();
  return (
    <Section
      icon={<PlugZapIcon />}
      title={<Trans id="admin.federation.diagnostics.title">Diagnostics</Trans>}
    >
      <ItemList
        label={t({
          id: "admin.federation.diagnostics.label",
          message: "Diagnostics",
        })}
        empty={null}
      >
        <EffectiveConfigRow slug={slug} />
        <ConnectionTestRow
          slug={slug}
          runId={runId}
          onRunIdChange={onRunIdChange}
        />
      </ItemList>
    </Section>
  );
}

function EffectiveConfigRow({ slug }: { slug: string }) {
  const [fetched, setFetched] = useState<EffectiveConfigView | null>(null);
  const [open, setOpen] = useState(false);
  // A failed read is reported by the query client's toast, like every other
  // failed request; the last good answer stays in the dialog, and the time it
  // was read says how old it is.
  const refresh = useMutation(refreshEffectiveConfigMutationOptions());

  async function read() {
    try {
      setFetched(await refresh.mutateAsync(slug));
      setOpen(true);
    } catch {
      // Already announced; nothing on the page changes.
    }
  }

  return (
    <ItemListRow
      icon={<FileSearchIcon aria-hidden="true" className="size-4" />}
      title={
        <Trans id="admin.federation.diagnostics.effective">
          Effective configuration
        </Trans>
      }
      details={[
        fetched === null ? (
          <Trans key="hint" id="admin.federation.diagnostics.effective.empty">
            Reads the endpoints and settings the provider is using now.
          </Trans>
        ) : (
          <Trans key="read" id="admin.federation.diagnostics.fetched">
            Read <RelativeTime value={fetched.fetchedAt} />
          </Trans>
        ),
      ]}
      stackActions
      actions={
        <Button
          pending={refresh.isPending && !open}
          onClick={() => void read()}
          variant="outline"
        >
          <Trans id="admin.federation.diagnostics.read">
            Read configuration
          </Trans>
        </Button>
      }
    >
      <EffectiveConfigDialog
        open={open && fetched !== null}
        onClose={() => setOpen(false)}
        fetched={fetched}
        pending={refresh.isPending}
        onReread={() => void read()}
      />
    </ItemListRow>
  );
}

function ConnectionTestRow({
  slug,
  runId,
  onRunIdChange,
}: {
  slug: string;
  runId: string | undefined;
  onRunIdChange: (runId: string | undefined) => void;
}) {
  const start = useMutation(startDiagnosticMutationOptions());
  // Only decorates the row: a summary that cannot be read leaves the badge out.
  const lastTest = useQuery(latestConnectionTestQueryOptions(slug));

  // Starting a test against a provider that cannot authenticate is refused by
  // the server with `federation_state_invalid`; the button stays available so
  // the message can say why.
  const begin = () => {
    void start.mutateAsync(slug).then(
      (run) => window.location.assign(run.authorizationUrl),
      () => undefined,
    );
  };
  // A started test leaves the page, so the button stays pending until it has
  // gone, and is let go if the browser brings the page back from its cache.
  const leaving = start.isPending || start.isSuccess;
  usePageRestore(() => start.reset());

  return (
    <ItemListRow
      icon={<PlugZapIcon aria-hidden="true" className="size-4" />}
      title={
        <Trans id="admin.federation.diagnostics.test">Connection test</Trans>
      }
      badges={<LastTestBadges test={lastTest.data?.latest} />}
      details={[
        <Trans key="hint" id="admin.federation.diagnostics.test.empty">
          Signs in once at the provider and checks each stage in turn.
        </Trans>,
      ]}
      stackActions
      actions={
        <Button
          pending={leaving && runId === undefined}
          onClick={begin}
          variant="outline"
        >
          <Trans id="admin.federation.diagnostics.start">Start a test</Trans>
          <ExternalLinkIcon aria-hidden="true" />
        </Button>
      }
    >
      <ConnectionTestDialog
        slug={slug}
        runId={runId}
        onClose={() => {
          start.reset();
          onRunIdChange(undefined);
        }}
        onRestart={begin}
        restarting={leaving}
      />
    </ItemListRow>
  );
}

/**
 * How the provider's latest finished test went, and whether its configuration
 * has changed since. Nothing before any test has finished.
 */
function LastTestBadges({
  test,
}: {
  test: LatestConnectionTestView | null | undefined;
}) {
  if (test === null || test === undefined) return null;
  const passed = test.status === "succeeded";
  return (
    <>
      <Badge variant={passed ? "success" : "error"}>
        {passed ? (
          <CircleCheckIcon aria-hidden="true" />
        ) : (
          <CircleXIcon aria-hidden="true" />
        )}
        {/* One element, so the badge's gap never splits the sentence. */}
        <span>
          {passed ? (
            <Trans id="admin.federation.diagnostics.last.passed">
              Passed <RelativeTime value={test.finishedAt} />
            </Trans>
          ) : (
            <Trans id="admin.federation.diagnostics.last.failed">
              Failed <RelativeTime value={test.finishedAt} />
            </Trans>
          )}
        </span>
      </Badge>
      {test.stale && (
        <Badge variant="warning">
          <TriangleAlertIcon aria-hidden="true" />
          <Trans id="admin.federation.claims.column.returned.stale">
            Configuration changed since
          </Trans>
        </Badge>
      )}
    </>
  );
}
