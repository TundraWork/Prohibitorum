import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CircleCheckIcon,
  CircleXIcon,
  ClockIcon,
  ExternalLinkIcon,
  RefreshCwIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { completeDiagnosticMutationOptions } from "@/api/mutations";
import { diagnosticResultQueryOptions } from "@/api/queries";
import type {
  DiagnosticDocumentView,
  DiagnosticFieldView,
  DiagnosticIdentityView,
  DiagnosticResultView,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { JsonBlock } from "@/components/custom/JsonBlock";
import { Alert, AlertAction, AlertTitle } from "@/components/ui/alert";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import {
  documentSourceLabel,
  identityFields,
  identityValue,
  type RunState,
  runState,
  runStatusLabel,
} from "@/pages/admin/identity-providers/provider-options";
import { SourcedList } from "@/pages/admin/identity-providers/SourcedList";
import { StageRow } from "@/pages/admin/identity-providers/StageRow";

/**
 * How long a test is watched before the reader is asked to check it again.
 *
 * A run happens partly in another browser window, so there is no way to know
 * from here whether it is still going. Polling forever would keep a request
 * alive for a test nobody came back to; a bounded window keeps the control
 * honest, and "Check again" covers the case where it did finish late.
 */
const pollIntervalSeconds = 1;
const pollWindowSeconds = 30;

/**
 * One connection test's result: the run's state, each stage in turn, then what
 * the provider sent. It is open while the page names a run, watches that run
 * for a bounded window, and acknowledges a finished one once.
 */
export function ConnectionTestDialog({
  slug,
  runId,
  onClose,
  onRestart,
  restarting,
}: {
  slug: string;
  /** The run named by `?test=`, or none while the dialog is closed. */
  runId: string | undefined;
  onClose: () => void;
  onRestart: () => void;
  restarting: boolean;
}) {
  const queryClient = useQueryClient();
  const popup = useRef<HTMLDivElement>(null);
  const complete = useMutation(completeDiagnosticMutationOptions(queryClient));

  // The window is measured from the clock rather than from how many answers have
  // arrived, and ends by a timer so the warning shows even when the last answer
  // changed nothing on screen.
  const [watching, setWatching] = useState(false);
  useEffect(() => {
    if (runId === undefined) {
      setWatching(false);
      return;
    }
    setWatching(true);
    const timer = setTimeout(
      () => setWatching(false),
      pollWindowSeconds * 1000,
    );
    return () => clearTimeout(timer);
  }, [runId]);

  // A finished run does not change again, and now carries both documents, so
  // polling stops as soon as it has a final status.
  const result = useQuery({
    ...diagnosticResultQueryOptions(slug, runId ?? ""),
    enabled: runId !== undefined,
    refetchInterval: (query) => {
      // A failed read is reported once; polling on would report it every second.
      if (query.state.status === "error") return false;
      const status = query.state.data?.status;
      if (status === "succeeded" || status === "failed") return false;
      return watching ? pollIntervalSeconds * 1000 : false;
    },
  });

  const unfinished =
    result.data?.status === "running" ||
    result.data?.status === "awaiting_callback";
  // A failed read ends the watch early: there is nothing left to wait for.
  const timedOut = unfinished && (!watching || result.isError);

  // A run is acknowledged once. The server advances its own state on `complete`,
  // so a second call would step it further than the reader asked for.
  const acknowledged = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (runId === undefined) return;
    if (result.data?.status !== "ready") return;
    if (acknowledged.current === runId) return;
    acknowledged.current = runId;
    complete.mutate({ slug, id: runId });
  }, [runId, result.data?.status, complete, slug]);

  function close() {
    acknowledged.current = undefined;
    onClose();
  }

  return (
    <Dialog
      open={runId !== undefined}
      onOpenChange={(next) => !next && !restarting && close()}
    >
      <DialogPopup
        ref={popup}
        className="sm:max-w-2xl"
        // The result is read, not answered: focus the dialog rather than a button.
        initialFocus={() => popup.current ?? true}
        showCloseButton={false}
      >
        <DialogHeader>
          <DialogTitle>
            <Trans id="admin.federation.diagnostics.test">
              Connection test
            </Trans>
          </DialogTitle>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-5">
          {result.isPending && (
            <div className="flex items-center justify-center py-6">
              <Spinner />
            </div>
          )}
          {timedOut && (
            <Alert variant="warning">
              <TriangleAlertIcon aria-hidden="true" />
              <AlertTitle>
                <Trans id="admin.federation.diagnostics.timeout">
                  This test is taking longer than expected. Check it again when
                  the provider has finished.
                </Trans>
              </AlertTitle>
              <AlertAction>
                <Button
                  pending={result.isFetching}
                  onClick={() => void result.refetch()}
                  variant="outline"
                  size="sm"
                >
                  <RefreshCwIcon aria-hidden="true" />
                  <Trans id="admin.federation.diagnostics.check-again">
                    Check again
                  </Trans>
                </Button>
              </AlertAction>
            </Alert>
          )}
          {result.data !== undefined && (
            <DiagnosticResult
              key={runId}
              result={result.data}
              stalled={timedOut}
            />
          )}
        </DialogPanel>
        <DialogFooter>
          <Button variant="ghost" disabled={restarting} onClick={close}>
            <Trans id="admin.federation.diagnostics.close">Close</Trans>
          </Button>
          <Button variant="outline" pending={restarting} onClick={onRestart}>
            <ExternalLinkIcon aria-hidden="true" />
            <Trans id="admin.federation.diagnostics.restart">Test again</Trans>
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

/**
 * The run's overall state, each stage in turn, then what the provider sent:
 * the fields login would map, and the two documents they were read from.
 */
function DiagnosticResult({
  result,
  stalled,
}: {
  result: DiagnosticResultView;
  /**
   * The watch window has closed while the run is still going, and the dialog
   * is saying so itself.
   */
  stalled: boolean;
}) {
  const { t } = useLingui();
  const state = runState(result.status);
  const hasDetails =
    result.identity !== undefined ||
    result.idToken !== undefined ||
    result.userinfo !== undefined;

  return (
    <div className="flex flex-col gap-5">
      {/* Once the watch has stalled the dialog's alert says so and carries
          Check again, so a second box saying the same thing is left out. */}
      {state !== undefined && !stalled && <RunStatus state={state} />}

      <ol
        aria-label={t({
          id: "admin.federation.diagnostics.stages",
          message: "Test stages",
        })}
        className="flex flex-col divide-y"
      >
        {(result.stages ?? []).map((stage) => (
          <StageRow key={stage.name} stage={stage} />
        ))}
      </ol>

      {hasDetails && <DiagnosticDetails result={result} />}
    </div>
  );
}

const runStatusStyle: Record<
  RunState,
  { variant: "default" | "success" | "error"; icon: ReactNode }
> = {
  waiting: { variant: "default", icon: <ClockIcon aria-hidden="true" /> },
  checking: { variant: "default", icon: <Spinner aria-hidden="true" /> },
  succeeded: {
    variant: "success",
    icon: <CircleCheckIcon aria-hidden="true" />,
  },
  failed: { variant: "error", icon: <CircleXIcon aria-hidden="true" /> },
};

function RunStatus({ state }: { state: RunState }) {
  const { i18n } = useLingui();
  const style = runStatusStyle[state];
  return (
    <Alert variant={style.variant} role="status">
      {style.icon}
      <AlertTitle>{i18n._(runStatusLabel(state))}</AlertTitle>
    </Alert>
  );
}

/**
 * Every tab's panel has the same height and scrolls inside it, so switching tabs
 * never changes the dialog's height and the tab strip stays under the pointer.
 */
const panelClass = "h-[min(20rem,45dvh)] overflow-y-auto pt-3";

/**
 * The mapped fields first, since that is what the reader came to check, then
 * the ID token and UserInfo to compare against the provider's own records. The
 * selection is local: the dialog keys the result on the run, so each new result
 * opens on the mapped fields, and `?test=` stays the only thing in the URL.
 */
function DiagnosticDetails({ result }: { result: DiagnosticResultView }) {
  const { t } = useLingui();
  const userinfoStage = result.stages?.find(
    (stage) => stage.name === "userinfo",
  );
  return (
    <Tabs defaultValue="identity">
      <TabsList
        variant="underline"
        aria-label={t({
          id: "admin.federation.diagnostics.details",
          message: "Test results",
        })}
      >
        <TabsTab value="identity">
          <Trans id="admin.federation.diagnostics.identity">
            Mapped fields
          </Trans>
        </TabsTab>
        <TabsTab value="id-token">
          <Trans id="admin.federation.diagnostics.stage.id-token">
            ID token
          </Trans>
        </TabsTab>
        <TabsTab value="userinfo">
          <Trans id="admin.federation.diagnostics.stage.userinfo">
            UserInfo
          </Trans>
        </TabsTab>
      </TabsList>
      <TabsPanel value="identity" className={panelClass}>
        {result.identity === undefined ? (
          <EmptyDetail>
            <Trans id="admin.federation.diagnostics.identity.empty">
              No fields could be mapped.
            </Trans>
          </EmptyDetail>
        ) : (
          <IdentityList identity={result.identity} />
        )}
      </TabsPanel>
      <TabsPanel value="id-token" className={panelClass}>
        <DocumentPanel
          document={result.idToken}
          empty={
            <Trans id="admin.federation.diagnostics.id-token.empty">
              The provider returned no ID token.
            </Trans>
          }
        />
      </TabsPanel>
      <TabsPanel value="userinfo" className={panelClass}>
        <DocumentPanel
          document={result.userinfo}
          empty={
            userinfoStage?.status === "failed" ? (
              <Trans id="admin.federation.diagnostics.userinfo.failed">
                UserInfo returned nothing to show.
              </Trans>
            ) : (
              <Trans id="admin.federation.diagnostics.userinfo.empty">
                UserInfo was not requested.
              </Trans>
            )
          }
        />
      </TabsPanel>
    </Tabs>
  );
}

function EmptyDetail({ children }: { children: ReactNode }) {
  return <p className="text-muted-foreground text-sm">{children}</p>;
}

function DocumentPanel({
  document,
  empty,
}: {
  document: DiagnosticDocumentView | undefined;
  empty: ReactNode;
}) {
  if (document?.json !== undefined) return <JsonBlock json={document.json} />;
  if (document?.omittedBytes !== undefined) {
    // Named, so the message carries `{size}`.
    const size = Math.ceil(document.omittedBytes / 1024);
    return (
      <EmptyDetail>
        <Trans id="admin.federation.diagnostics.document.omitted">
          Too large to keep ({size} KB).
        </Trans>
      </EmptyDetail>
    );
  }
  return <EmptyDetail>{empty}</EmptyDetail>;
}

/**
 * The fields login would store, each with the document and claim it came from.
 * The source says the rest: every row on UserInfo means the provider sent no ID
 * token, and only the picture on UserInfo means it was filled in from there.
 */
function IdentityList({ identity }: { identity: DiagnosticIdentityView }) {
  const { i18n } = useLingui();
  const rows = identityFields.map(({ key, label }) => ({
    key,
    label: i18n._(label),
    value: <IdentityFieldValue field={identity[key]} />,
    // A value that never arrived has no source to name.
    source:
      identityValue(identity[key]).kind === "missing" ? undefined : (
        <FieldSource field={identity[key]} />
      ),
  }));
  return <SourcedList rows={rows} />;
}

function IdentityFieldValue({ field }: { field: DiagnosticFieldView }) {
  const value = identityValue(field);
  switch (value.kind) {
    case "missing":
      return (
        <span className="text-muted-foreground text-sm">
          <Trans id="admin.federation.diagnostics.identity.missing">
            Not provided
          </Trans>
        </span>
      );
    case "boolean":
      return (
        <span className="text-sm">
          {value.value ? (
            <Trans id="admin.federation.diagnostics.identity.yes">Yes</Trans>
          ) : (
            <Trans id="admin.federation.diagnostics.identity.no">No</Trans>
          )}
        </span>
      );
    case "text":
      return (
        <span className="wrap-anywhere font-mono text-[13px]">
          {value.text}
        </span>
      );
  }
}

function FieldSource({ field }: { field: DiagnosticFieldView }) {
  const { i18n } = useLingui();
  const document = documentSourceLabel(field.source);
  const name = document === undefined ? field.source : i18n._(document);
  if (field.claim === undefined) return <>{name}</>;
  // One sentence per source, so a language can order the document and claim
  // its own way.
  const claim = field.claim;
  return (
    <Trans id="admin.federation.diagnostics.field-source">
      {name} · <span className="font-mono">{claim}</span>
    </Trans>
  );
}
