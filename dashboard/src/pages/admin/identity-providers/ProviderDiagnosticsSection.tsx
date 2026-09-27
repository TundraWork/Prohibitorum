import { Chip, Modal, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Circle,
  CircleCheck,
  CircleMinus,
  CircleX,
  Clock,
  ExternalLink,
  FileSearch,
  PlugZap,
  RefreshCw,
} from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import {
  completeDiagnosticMutationOptions,
  refreshEffectiveConfigMutationOptions,
  startDiagnosticMutationOptions,
} from "@/api/mutations";
import { diagnosticResultQueryOptions } from "@/api/queries";
import type {
  DiagnosticResultView,
  DiagnosticStageView,
  EffectiveConfigField,
  EffectiveConfigView,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { CopyValue } from "@/components/custom/CopyValue";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  configSourceLabel,
  effectiveConfigFields,
  type RunState,
  runState,
  runStatusLabel,
  type StageState,
  stageName,
  stageState,
  stageStatusLabel,
} from "@/pages/admin/identity-providers/provider-options";

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

/** `?test=<id>` is 43 base64url characters; anything else is ignored. */
const runIdPattern = /^[A-Za-z0-9_-]{43}$/;

export function readRunId(value: unknown): string | undefined {
  return typeof value === "string" && runIdPattern.test(value)
    ? value
    : undefined;
}

/**
 * Whether the upstream actually works, before anyone depends on it.
 *
 * Two actions, one row each, drawn the way the danger zone below draws its
 * own: the effective configuration answers "what did we resolve, and where
 * from", and the connection test answers "does a sign-in go through". What
 * each one finds opens in a dialog rather than growing under the row, so the
 * section keeps the same short shape before and after, and a long table of
 * endpoints does not push the danger zone down the page.
 *
 * Neither is fetched on first paint: both reach the upstream, and the server
 * rate-limits them (twenty a minute for the configuration, six for a test), so
 * both are reads the reader asks for. The test leaves the page: the browser is
 * sent to the provider's authorization endpoint and comes back with
 * `?test=<id>`, which is the only way this page learns that a run exists, and
 * the result dialog opens for that run. A request that fails says so in the
 * console's error toast and nowhere else, so the rows never change shape.
 */
export function ProviderDiagnosticsSection({
  slug,
  runId,
  onRunIdChange,
}: {
  slug: string;
  /** The run named by `?test=`, when the page was opened with one. */
  runId: string | undefined;
  onRunIdChange: (runId: string | undefined) => void;
}) {
  const { t } = useLingui();
  return (
    <Section
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

/**
 * A diagnostic's findings in a dialog: HeroUI's anatomy, with the body
 * scrolling under a footer that keeps "again" and "close" in reach.
 */
function ResultDialog({
  isOpen,
  onClose,
  title,
  footer,
  children,
}: {
  isOpen: boolean;
  onClose: () => void;
  title: ReactNode;
  footer: ReactNode;
  children: ReactNode;
}) {
  return (
    <Modal
      isOpen={isOpen}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Modal.Backdrop>
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>{title}</Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              <div className="flex flex-col gap-4">{children}</div>
            </Modal.Body>
            <Modal.Footer className="mt-5">{footer}</Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
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

  const title = (
    <Trans id="admin.federation.diagnostics.effective">
      Effective configuration
    </Trans>
  );

  return (
    <ItemListRow
      icon={<FileSearch size={18} aria-hidden="true" />}
      title={title}
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
      actions={
        <Button
          size="sm"
          variant="secondary"
          isPending={refresh.isPending && !open}
          onPress={() => void read()}
        >
          <Trans id="admin.federation.diagnostics.read">
            Read configuration
          </Trans>
        </Button>
      }
    >
      <ResultDialog
        isOpen={open && fetched !== null}
        onClose={() => setOpen(false)}
        title={title}
        footer={
          <>
            <Button variant="secondary" onPress={() => setOpen(false)}>
              <Trans id="admin.federation.diagnostics.close">Close</Trans>
            </Button>
            <Button
              variant="secondary"
              isPending={refresh.isPending}
              onPress={() => void read()}
            >
              <RefreshCw size={16} aria-hidden="true" />
              <Trans id="admin.federation.diagnostics.reread">Read again</Trans>
            </Button>
          </>
        }
      >
        {fetched !== null && (
          <>
            <p className="text-xs text-muted">
              <Trans id="admin.federation.diagnostics.fetched">
                Read <RelativeTime value={fetched.fetchedAt} />
              </Trans>
            </p>
            <EffectiveConfigTable fields={fetched.fields} />
            <CopyValue
              value={fetched.callbackUrl}
              label={
                <Trans id="admin.federation.diagnostics.callback">
                  Test callback address
                </Trans>
              }
              description={
                <Trans id="admin.federation.diagnostics.callback.hint">
                  Register this address with the provider before running a
                  connection test.
                </Trans>
              }
            />
          </>
        )}
      </ResultDialog>
    </ItemListRow>
  );
}

/**
 * The resolved values as a three-column list: what the value is, the value,
 * and where it came from. It switches on its own width: wide, the three line
 * up across the card; narrow, the name and its source share a line over the
 * value, which is the column that needs the room.
 */
function EffectiveConfigTable({
  fields,
}: {
  fields: Record<string, EffectiveConfigField>;
}) {
  const { i18n } = useLingui();
  return (
    <div className="@container/effective">
      <dl className="flex flex-col divide-y divide-separator">
        {effectiveConfigFields.map(({ key, label }) => {
          const field = fields[key];
          if (field === undefined) return null;
          const source = configSourceLabel(field.source);
          return (
            <div
              key={key}
              className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 py-2.5 first:pt-0 last:pb-0 @xl/effective:grid-cols-[9rem_minmax(0,1fr)_auto] @xl/effective:gap-x-6"
            >
              <dt className="text-sm text-muted">{i18n._(label)}</dt>
              <dd className="col-span-2 row-start-2 min-w-0 @xl/effective:col-span-1 @xl/effective:col-start-2 @xl/effective:row-start-1">
                <EffectiveValue value={field.value} />
              </dd>
              <dd className="col-start-2 row-start-1 justify-self-end @xl/effective:col-start-3">
                {source !== undefined && (
                  <Chip size="sm" variant="soft">
                    <Chip.Label>{i18n._(source)}</Chip.Label>
                  </Chip>
                )}
              </dd>
            </div>
          );
        })}
      </dl>
    </div>
  );
}

function EffectiveValue({ value }: { value: string | string[] | null }) {
  const empty =
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0);
  if (empty) {
    return (
      <span className="text-sm text-muted">
        <Trans id="admin.federation.diagnostics.unused">Not used</Trans>
      </span>
    );
  }
  if (Array.isArray(value)) {
    return (
      <span className="flex flex-wrap gap-1.5">
        {value.map((item) => (
          <Chip key={item} size="sm" variant="secondary">
            <Chip.Label className="font-mono">{item}</Chip.Label>
          </Chip>
        ))}
      </span>
    );
  }
  return (
    <span className="font-mono text-sm break-all text-foreground">{value}</span>
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
  const queryClient = useQueryClient();
  const start = useMutation(startDiagnosticMutationOptions());
  const complete = useMutation(completeDiagnosticMutationOptions(queryClient));

  // The window is measured from the clock rather than from how many answers have
  // arrived: a slow upstream and a fast one both get the same thirty seconds, and
  // a request that never returns cannot keep the window open indefinitely.
  const [deadline, setDeadline] = useState<number | null>(null);
  useEffect(() => {
    if (runId === undefined) {
      setDeadline(null);
      return;
    }
    setDeadline(Date.now() + pollWindowSeconds * 1000);
  }, [runId]);

  const result = useQuery({
    ...diagnosticResultQueryOptions(slug, runId ?? ""),
    enabled: runId !== undefined,
    refetchInterval:
      runId !== undefined && deadline !== null && Date.now() < deadline
        ? pollIntervalSeconds * 1000
        : false,
  });

  // Whether the window has closed while the run is still going. Recomputed on
  // each render, and a render happens on every poll that returns.
  const running = result.data?.status === "running";
  const timedOut = running && deadline !== null && Date.now() >= deadline;

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

  // Starting a test against a provider that cannot authenticate is refused by
  // the server with `federation_state_invalid`; the button stays available so
  // the message can say why.
  const begin = () => {
    void start.mutateAsync(slug).then(
      (run) => window.location.assign(run.authorizationUrl),
      () => undefined,
    );
  };

  const close = () => {
    acknowledged.current = undefined;
    start.reset();
    onRunIdChange(undefined);
  };

  const title = (
    <Trans id="admin.federation.diagnostics.test">Connection test</Trans>
  );

  return (
    <ItemListRow
      icon={<PlugZap size={18} aria-hidden="true" />}
      title={title}
      details={[
        <Trans key="hint" id="admin.federation.diagnostics.test.empty">
          Sign in once at the provider and check each stage in turn.
        </Trans>,
      ]}
      actions={
        <Button
          size="sm"
          variant="secondary"
          isPending={start.isPending && runId === undefined}
          onPress={begin}
        >
          <ExternalLink size={16} aria-hidden="true" />
          <Trans id="admin.federation.diagnostics.start">Start a test</Trans>
        </Button>
      }
    >
      <ResultDialog
        isOpen={runId !== undefined}
        onClose={close}
        title={title}
        footer={
          <>
            <Button variant="secondary" onPress={close}>
              <Trans id="admin.federation.diagnostics.close">Close</Trans>
            </Button>
            <Button
              variant="secondary"
              isPending={start.isPending}
              onPress={begin}
            >
              <ExternalLink size={16} aria-hidden="true" />
              <Trans id="admin.federation.diagnostics.restart">
                Test again
              </Trans>
            </Button>
          </>
        }
      >
        {result.isPending && (
          <div className="flex items-center justify-center py-6">
            <Spinner size="md" />
          </div>
        )}
        {timedOut && (
          <SurfaceAlert status="warning" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="admin.federation.diagnostics.timeout">
                  This test is taking longer than expected. Check it again when
                  the provider has finished.
                </Trans>
              </SurfaceAlert.Title>
              <Button
                className="mt-2 w-fit"
                size="sm"
                variant="outline"
                isPending={result.isFetching}
                onPress={() => void result.refetch()}
              >
                <RefreshCw size={16} aria-hidden="true" />
                <Trans id="admin.federation.diagnostics.check-again">
                  Check again
                </Trans>
              </Button>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}
        {result.data !== undefined && <DiagnosticResult result={result.data} />}
      </ResultDialog>
    </ItemListRow>
  );
}

/** The run's overall state, each stage in turn, then the claims it returned. */
function DiagnosticResult({ result }: { result: DiagnosticResultView }) {
  const { t } = useLingui();
  const state = runState(result.status);
  const claims = Object.entries(result.claims ?? {});

  return (
    <div className="flex flex-col gap-4">
      {state !== undefined && <RunStatus state={state} />}

      <ol
        aria-label={t({
          id: "admin.federation.diagnostics.stages",
          message: "Test stages",
        })}
        className="flex flex-col divide-y divide-separator"
      >
        {(result.stages ?? []).map((stage) => (
          <StageRow key={stage.name} stage={stage} />
        ))}
      </ol>

      {claims.length > 0 && (
        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-medium text-muted">
            <Trans id="admin.federation.diagnostics.claims">
              Claims returned by the provider
            </Trans>
          </h4>
          <dl className="grid grid-cols-[minmax(0,max-content)_minmax(0,1fr)] gap-x-6 gap-y-1.5">
            {claims.map(([name, value]) => (
              <div key={name} className="contents">
                <dt className="font-mono text-sm text-muted break-all">
                  {name}
                </dt>
                <dd className="font-mono text-sm break-all text-foreground">
                  {typeof value === "string" ? value : JSON.stringify(value)}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      )}
    </div>
  );
}

function RunStatus({ state }: { state: RunState }) {
  const { i18n } = useLingui();
  const tone: Record<RunState, string> = {
    waiting: "text-muted",
    checking: "text-foreground",
    succeeded: "text-success",
    failed: "text-danger",
  };
  return (
    <p
      role="status"
      className={`flex items-center gap-2 text-sm font-medium ${tone[state]}`}
    >
      {state === "checking" ? (
        <Spinner size="sm" />
      ) : state === "succeeded" ? (
        <CircleCheck size={16} aria-hidden="true" />
      ) : state === "failed" ? (
        <CircleX size={16} aria-hidden="true" />
      ) : (
        <Clock size={16} aria-hidden="true" />
      )}
      {i18n._(runStatusLabel(state))}
    </p>
  );
}

const stageIcon: Record<StageState, ReactNode> = {
  succeeded: (
    <CircleCheck size={16} className="text-success" aria-hidden="true" />
  ),
  failed: <CircleX size={16} className="text-danger" aria-hidden="true" />,
  skipped: <CircleMinus size={16} className="text-muted" aria-hidden="true" />,
  pending: <Circle size={16} className="text-muted" aria-hidden="true" />,
};

/**
 * One stage: its outcome as an icon and a word, its name, and what the server
 * measured. The details name themselves ("HTTP 400", "Request ID …") rather
 * than leaning on column headings, and the literal ones — the error code, the
 * endpoint, the request ID — are monospace so they can be matched against the
 * provider's own logs.
 */
function StageRow({ stage }: { stage: DiagnosticStageView }) {
  const { i18n } = useLingui();
  const state = stageState(stage.status);
  const name = stageName(stage.name);
  // Named, so the messages carry `{duration}` and `{requestId}`.
  const duration = stage.durationMs;
  const requestId = stage.requestId;
  const details: ReactNode[] = [
    duration === undefined ? null : (
      <span key="duration" className="tabular-nums">
        <Trans id="admin.federation.diagnostics.stage.duration">
          {duration} ms
        </Trans>
      </span>
    ),
    stage.httpStatus === undefined ? null : (
      <span key="http" className="tabular-nums">
        HTTP {stage.httpStatus}
      </span>
    ),
    stage.errorCode === undefined ? null : (
      <span key="code" className="font-mono">
        {stage.errorCode}
      </span>
    ),
    stage.endpoint === undefined ? null : (
      <span key="endpoint" className="font-mono break-all">
        {stage.endpoint}
      </span>
    ),
    requestId === undefined ? null : (
      <span key="request">
        <Trans id="admin.federation.diagnostics.stage.request">
          Request ID <span className="font-mono">{requestId}</span>
        </Trans>
      </span>
    ),
  ].filter((detail) => detail !== null);

  return (
    <li className="grid grid-cols-[1rem_minmax(0,1fr)_auto] items-start gap-x-3 py-2.5 first:pt-0 last:pb-0">
      <span className="flex h-5 items-center">{stageIcon[state]}</span>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium text-foreground">
          {name === undefined ? stage.name : i18n._(name)}
        </span>
        {details.length > 0 && (
          <span className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted">
            {details}
          </span>
        )}
      </div>
      <span
        // Only a failure is coloured: the icons already mark every outcome, and
        // a column of green would bury the one stage that needs reading.
        className={`text-xs leading-5 ${
          state === "failed" ? "text-danger" : "text-muted"
        }`}
      >
        {i18n._(stageStatusLabel(state))}
      </span>
    </li>
  );
}
