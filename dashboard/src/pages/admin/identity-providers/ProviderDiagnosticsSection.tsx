import { Chip, Modal, Spinner, Tabs } from "@heroui/react";
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
  DiagnosticDocumentView,
  DiagnosticFieldView,
  DiagnosticIdentityView,
  DiagnosticResultView,
  DiagnosticStageView,
  EffectiveConfigField,
  EffectiveConfigView,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { CopyValue } from "@/components/custom/CopyValue";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { JsonBlock } from "@/components/custom/JsonBlock";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  configSourceLabel,
  documentSourceLabel,
  effectiveConfigFields,
  identityFields,
  identityValue,
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

interface SourcedRow {
  key: string;
  label: ReactNode;
  value: ReactNode;
  source: ReactNode;
}

/**
 * A three-column list of what a value is, the value, and where it came from,
 * shared by both diagnostics so they read alike. It switches on its own width:
 * wide, the three line up across the dialog; narrow, the name and its source
 * share a line over the value, which is the column that needs the room.
 */
function SourcedList({ rows }: { rows: readonly SourcedRow[] }) {
  return (
    <div className="@container/sourced">
      <dl className="flex flex-col divide-y divide-separator">
        {rows.map((row) => (
          <div
            key={row.key}
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 py-2.5 first:pt-0 last:pb-0 @xl/sourced:grid-cols-[9rem_minmax(0,1fr)_auto] @xl/sourced:gap-x-6"
          >
            <dt className="text-sm text-muted">{row.label}</dt>
            <dd className="col-span-2 row-start-2 min-w-0 @xl/sourced:col-span-1 @xl/sourced:col-start-2 @xl/sourced:row-start-1">
              {row.value}
            </dd>
            <dd className="col-start-2 row-start-1 justify-self-end @xl/sourced:col-start-3">
              {row.source !== undefined && row.source !== null && (
                <Chip size="sm" variant="soft">
                  <Chip.Label>{row.source}</Chip.Label>
                </Chip>
              )}
            </dd>
          </div>
        ))}
      </dl>
    </div>
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
        value: <EffectiveValue value={field.value} />,
        source: source === undefined ? undefined : i18n._(source),
      },
    ];
  });
  return <SourcedList rows={rows} />;
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

  // A finished run does not change again, and now carries both documents, so
  // polling stops as soon as it has a final status.
  const result = useQuery({
    ...diagnosticResultQueryOptions(slug, runId ?? ""),
    enabled: runId !== undefined,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      if (status === "succeeded" || status === "failed") return false;
      return runId !== undefined && deadline !== null && Date.now() < deadline
        ? pollIntervalSeconds * 1000
        : false;
    },
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
        {result.data !== undefined && (
          <DiagnosticResult key={runId} result={result.data} />
        )}
      </ResultDialog>
    </ItemListRow>
  );
}

/**
 * The run's overall state, each stage in turn, then what the provider sent:
 * the fields login would map, and the two documents they were read from.
 */
function DiagnosticResult({ result }: { result: DiagnosticResultView }) {
  const { t } = useLingui();
  const state = runState(result.status);
  const hasDetails =
    result.identity !== undefined ||
    result.idToken !== undefined ||
    result.userinfo !== undefined;

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

      {hasDetails && <DiagnosticDetails result={result} />}
    </div>
  );
}

/**
 * The mapped fields first, since that is what the reader came to check, then
 * the ID token and UserInfo to compare against the provider's own records.
 * The selection is local: the dialog is keyed on the run, so each new result
 * opens on the mapped fields, and `?test=` stays the only thing in the URL.
 */
const tabClass = "whitespace-nowrap @max-sm/details:px-3";

function DiagnosticDetails({ result }: { result: DiagnosticResultView }) {
  const { t } = useLingui();
  const userinfoStage = result.stages?.find(
    (stage) => stage.name === "userinfo",
  );
  return (
    // On a phone the three labels overrun the dialog by a few pixels at
    // HeroUI's tab padding; a step tighter keeps them all in view down to a
    // 360px screen, and the library's scroller covers anything narrower.
    <Tabs
      variant="secondary"
      defaultSelectedKey="identity"
      className="@container/details"
    >
      {/* The secondary variant draws its baseline on the container, so the
          container keeps the dialog's width; the list gives up its default
          full-width minimum so each tab takes its label's width. */}
      <Tabs.ListContainer className="max-w-full">
        <Tabs.List
          className="w-fit min-w-0"
          aria-label={t({
            id: "admin.federation.diagnostics.details",
            message: "Test results",
          })}
        >
          <Tabs.Tab id="identity" className={tabClass}>
            <Trans id="admin.federation.diagnostics.identity">
              Mapped fields
            </Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab id="id-token" className={tabClass}>
            <Trans id="admin.federation.diagnostics.stage.id-token">
              ID token
            </Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab id="userinfo" className={tabClass}>
            <Trans id="admin.federation.diagnostics.stage.userinfo">
              UserInfo
            </Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
        </Tabs.List>
      </Tabs.ListContainer>
      <Tabs.Panel id="identity" className="pt-4">
        {result.identity === undefined ? (
          <EmptyDetail>
            <Trans id="admin.federation.diagnostics.identity.empty">
              No fields could be mapped.
            </Trans>
          </EmptyDetail>
        ) : (
          <IdentityList identity={result.identity} />
        )}
      </Tabs.Panel>
      <Tabs.Panel id="id-token" className="pt-4">
        <DocumentPanel
          document={result.idToken}
          empty={
            <Trans id="admin.federation.diagnostics.id-token.empty">
              The provider returned no ID token.
            </Trans>
          }
        />
      </Tabs.Panel>
      <Tabs.Panel id="userinfo" className="pt-4">
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
      </Tabs.Panel>
    </Tabs>
  );
}

function EmptyDetail({ children }: { children: ReactNode }) {
  return <p className="text-sm text-muted">{children}</p>;
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
 * The fields login would store, each with the document and claim it came
 * from. The source says the rest: every row on UserInfo means the provider
 * sent no ID token, and only the picture on UserInfo means it was filled in
 * from there.
 */
function IdentityList({ identity }: { identity: DiagnosticIdentityView }) {
  const { i18n } = useLingui();
  const rows = identityFields.map(({ key, label }) => ({
    key,
    label: i18n._(label),
    value: <IdentityFieldValue field={identity[key]} />,
    source: <FieldSource field={identity[key]} />,
  }));
  return <SourcedList rows={rows} />;
}

function IdentityFieldValue({ field }: { field: DiagnosticFieldView }) {
  const value = identityValue(field);
  switch (value.kind) {
    case "missing":
      return (
        <span className="text-sm text-muted">
          <Trans id="admin.federation.diagnostics.identity.missing">
            Not provided
          </Trans>
        </span>
      );
    case "boolean":
      return (
        <span className="text-sm text-foreground">
          {value.value ? (
            <Trans id="admin.federation.diagnostics.identity.yes">Yes</Trans>
          ) : (
            <Trans id="admin.federation.diagnostics.identity.no">No</Trans>
          )}
        </span>
      );
    case "text":
      return (
        <span className="font-mono text-sm break-all text-foreground">
          {value.text}
        </span>
      );
  }
}

function FieldSource({ field }: { field: DiagnosticFieldView }) {
  const { i18n } = useLingui();
  const document = documentSourceLabel(field.source);
  return (
    <>
      {document === undefined ? field.source : i18n._(document)}
      {field.claim !== undefined && (
        <>
          {" · "}
          <span className="font-mono">{field.claim}</span>
        </>
      )}
    </>
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
