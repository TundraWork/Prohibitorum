import { Chip, Separator, Spinner } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import {
  completeDiagnosticMutationOptions,
  refreshEffectiveConfigMutationOptions,
  startDiagnosticMutationOptions,
} from "@/api/mutations";
import { diagnosticResultQueryOptions } from "@/api/queries";
import type {
  DiagnosticResultView,
  DiagnosticStageView,
  EffectiveConfigView,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { CopyValue } from "@/components/custom/CopyValue";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * How long a test is watched before the reader is asked to refresh instead.
 *
 * A run happens partly in another browser window, so there is no way to know
 * from here whether it is still going. Polling forever would keep a request
 * alive for a test nobody came back to; a bounded window keeps the control
 * honest, and the refresh button covers the case where it did finish late.
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
 * Two questions, one card each. The effective configuration answers "what did we
 * resolve, and where from" — the endpoint discovery found, the one an operator
 * overrode, the one written by hand — and the connection test answers "does a
 * sign-in go through". Neither is fetched on first paint: both reach the
 * upstream, and the server rate-limits them (twenty a minute for the
 * configuration, six for a test), so both are reads the reader asks for.
 *
 * The test leaves the page. The browser is sent to the provider's authorization
 * endpoint and comes back here with `?test=<id>`, which is the only way this page
 * learns that a run exists; the id is read from the URL once, when the page
 * mounts with it.
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
  return (
    <>
      <EffectiveConfigCard slug={slug} />
      <ConnectionTestCard
        slug={slug}
        runId={runId}
        onRunIdChange={onRunIdChange}
      />
    </>
  );
}

function EffectiveConfigCard({ slug }: { slug: string }) {
  const { t } = useLingui();
  const [fetched, setFetched] = useState<EffectiveConfigView | null>(null);
  const refresh = useMutation(refreshEffectiveConfigMutationOptions());
  // The last good answer stays on screen when a refresh fails: it is still the
  // configuration in force, and blanking it would say less than the error does.
  const [staleFailure, setStaleFailure] = useState<unknown>(null);

  async function onRefresh() {
    setStaleFailure(null);
    try {
      setFetched(await refresh.mutateAsync(slug));
    } catch (error) {
      if (isCancellation(error)) return;
      setStaleFailure(error);
    }
  }

  return (
    <ConsoleCard>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-3">
          <Button
            size="sm"
            variant="outline"
            isPending={refresh.isPending}
            onPress={() => void onRefresh()}
          >
            <RefreshCw size={16} aria-hidden="true" />
            <Trans id="admin.federation.diagnostics.refresh">Refresh</Trans>
          </Button>
          {fetched !== null && (
            <span className="text-xs text-muted">
              <Trans id="admin.federation.diagnostics.fetched">
                Read <RelativeTime value={fetched.fetchedAt} />
              </Trans>
            </span>
          )}
        </div>

        {staleFailure !== null && (
          <SurfaceAlert status="warning" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                {t(describeError(staleFailure))}
              </SurfaceAlert.Title>
              <SurfaceAlert.Description>
                <Trans id="admin.federation.diagnostics.stale">
                  Showing the last configuration that was read.
                </Trans>
              </SurfaceAlert.Description>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {fetched !== null && (
          <>
            <CopyValue
              value={fetched.callbackUrl}
              label={
                <Trans id="admin.federation.diagnostics.callback">
                  Callback address
                </Trans>
              }
              description={
                <Trans id="admin.federation.diagnostics.callback.hint">
                  Register this address with the provider.
                </Trans>
              }
            />
            <Separator />
            <dl className="flex flex-col gap-3">
              {Object.entries(fetched.fields).map(([name, field]) => (
                <div key={name} className="flex flex-col gap-1">
                  <dt className="text-xs text-muted">{name}</dt>
                  <dd className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-sm">
                      {Array.isArray(field.value)
                        ? field.value.join(" ")
                        : field.value}
                    </span>
                    <Chip size="sm" variant="soft">
                      {field.source === "discovery" ? (
                        <Trans id="admin.federation.diagnostics.source.discovery">
                          Discovered
                        </Trans>
                      ) : field.source === "override" ? (
                        <Trans id="admin.federation.diagnostics.source.override">
                          Overridden
                        </Trans>
                      ) : (
                        <Trans id="admin.federation.diagnostics.source.manual">
                          Entered by hand
                        </Trans>
                      )}
                    </Chip>
                  </dd>
                </div>
              ))}
            </dl>
          </>
        )}
      </div>
    </ConsoleCard>
  );
}

function ConnectionTestCard({
  slug,
  runId,
  onRunIdChange,
}: {
  slug: string;
  runId: string | undefined;
  onRunIdChange: (runId: string | undefined) => void;
}) {
  const { t } = useLingui();
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

  return (
    <ConsoleCard>
      <div className="flex flex-col gap-4">
        {runId === undefined ? (
          <div>
            <Button
              size="sm"
              isPending={start.isPending}
              // Starting a test against a provider that cannot authenticate is
              // refused by the server with `federation_state_invalid`; the
              // button stays available so the message can say why.
              onPress={() => {
                void start.mutateAsync(slug).then(
                  (run) => window.location.assign(run.authorizationUrl),
                  () => undefined,
                );
              }}
            >
              <ExternalLink size={16} aria-hidden="true" />
              <Trans id="admin.federation.diagnostics.start">
                Start a test
              </Trans>
            </Button>
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <Button
              size="sm"
              variant="outline"
              isPending={result.isFetching}
              onPress={() => void result.refetch()}
            >
              <RefreshCw size={16} aria-hidden="true" />
              <Trans id="admin.federation.diagnostics.refresh-result">
                Refresh result
              </Trans>
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onPress={() => {
                acknowledged.current = undefined;
                onRunIdChange(undefined);
              }}
            >
              <Trans id="admin.federation.diagnostics.close">Close</Trans>
            </Button>
          </div>
        )}

        {start.error !== null && start.error !== undefined && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                {t(describeError(start.error))}
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {runId !== undefined && (
          <>
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
                      This test is taking longer than expected. Check it again
                      when the provider has finished.
                    </Trans>
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
            )}
            {result.data !== undefined && (
              <DiagnosticStages result={result.data} />
            )}
          </>
        )}
      </div>
    </ConsoleCard>
  );
}

/** One row per stage, then the claims the run returned. */
function DiagnosticStages({ result }: { result: DiagnosticResultView }) {
  const { i18n } = useLingui();
  return (
    <>
      <ItemList
        label={i18n._(
          msg({
            id: "admin.federation.diagnostics.stages",
            message: "Test stages",
          }),
        )}
        empty={null}
      >
        {(result.stages ?? []).map((stage) => (
          <ItemListRow
            key={stage.name}
            title={stageName(stage)}
            badges={
              <Chip
                size="sm"
                variant="soft"
                color={stage.status === "failed" ? "danger" : "default"}
              >
                {stage.status}
              </Chip>
            }
            details={[
              ...(stage.endpoint === undefined ? [] : [stage.endpoint]),
              ...(stage.durationMs === undefined
                ? []
                : [`${stage.durationMs} ms`]),
              ...(stage.httpStatus === undefined
                ? []
                : [`HTTP ${stage.httpStatus}`]),
              ...(stage.errorCode === undefined ? [] : [stage.errorCode]),
              ...(stage.requestId === undefined ? [] : [stage.requestId]),
            ]}
          />
        ))}
      </ItemList>
      {result.claims !== undefined && (
        <dl className="flex flex-col gap-1">
          {Object.entries(result.claims).map(([name, value]) => (
            <div key={name} className="flex items-baseline gap-2">
              <dt className="text-xs text-muted">{name}</dt>
              <dd className="font-mono text-sm">{String(value)}</dd>
            </div>
          ))}
        </dl>
      )}
    </>
  );
}

/** The server's stage names, as the reader knows them. */
function stageName(stage: DiagnosticStageView) {
  switch (stage.name) {
    case "discovery":
      return (
        <Trans id="admin.federation.diagnostics.stage.discovery">
          Discovery
        </Trans>
      );
    case "authorize":
      return (
        <Trans id="admin.federation.diagnostics.stage.authorize">
          Authorization
        </Trans>
      );
    case "callback":
      return (
        <Trans id="admin.federation.diagnostics.stage.callback">Callback</Trans>
      );
    case "token_exchange":
      return (
        <Trans id="admin.federation.diagnostics.stage.token">
          Token exchange
        </Trans>
      );
    case "id_token":
      return (
        <Trans id="admin.federation.diagnostics.stage.id-token">ID token</Trans>
      );
    case "userinfo":
      return (
        <Trans id="admin.federation.diagnostics.stage.userinfo">UserInfo</Trans>
      );
    default:
      return stage.name;
  }
}
