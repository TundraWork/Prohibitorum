import { Trans, useLingui } from "@lingui/react/macro";
import {
  CircleCheckIcon,
  CircleIcon,
  CircleMinusIcon,
  CircleXIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import type { DiagnosticStageView } from "@/api/raw-admin-paths";
import {
  type StageState,
  stageName,
  stageState,
  stageStatusLabel,
} from "@/pages/admin/identity-providers/provider-options";

const stageIcon: Record<StageState, ReactNode> = {
  succeeded: (
    <CircleCheckIcon aria-hidden="true" className="size-4 text-success" />
  ),
  failed: (
    <CircleXIcon aria-hidden="true" className="size-4 text-destructive" />
  ),
  skipped: (
    <CircleMinusIcon
      aria-hidden="true"
      className="size-4 text-muted-foreground"
    />
  ),
  pending: (
    <CircleIcon aria-hidden="true" className="size-4 text-muted-foreground" />
  ),
};

/**
 * One stage: its outcome as an icon and a word, its name, and what the server
 * measured. The details name themselves ("HTTP 400", "Request ID …") rather
 * than leaning on column headings, and the literal ones — the error code, the
 * endpoint, the request ID — are monospace so they can be matched against the
 * provider's own logs. Every value wraps, so none loses its distinguishing end.
 */
export function StageRow({ stage }: { stage: DiagnosticStageView }) {
  const { i18n } = useLingui();
  const state = stageState(stage.status);
  const name = stageName(stage.name);
  // Named, so the messages carry `{duration}` and `{requestId}`.
  const duration = stage.durationMs;
  const requestId = stage.requestId;
  const httpStatus = stage.httpStatus;
  const details: ReactNode[] = [
    duration === undefined ? null : (
      <span key="duration" className="tabular-nums">
        <Trans id="admin.federation.diagnostics.stage.duration">
          {duration} ms
        </Trans>
      </span>
    ),
    httpStatus === undefined ? null : (
      <span key="http" className="tabular-nums">
        <Trans id="admin.federation.diagnostics.stage.http">
          HTTP {httpStatus}
        </Trans>
      </span>
    ),
    stage.errorCode === undefined ? null : (
      <span key="code" className="font-mono wrap-anywhere">
        {stage.errorCode}
      </span>
    ),
    stage.endpoint === undefined ? null : (
      <span key="endpoint" className="font-mono wrap-anywhere">
        {stage.endpoint}
      </span>
    ),
    requestId === undefined ? null : (
      <span key="request" className="wrap-anywhere">
        <Trans id="admin.federation.diagnostics.stage.request">
          Request ID <span className="font-mono">{requestId}</span>
        </Trans>
      </span>
    ),
  ].filter((detail) => detail !== null);

  // The provider's own description of the failure (e.g. Microsoft's AADSTS
  // text) is a sentence, not a literal: it sits on its own line under a label
  // and wraps, so a long URL inside it cannot push the stage list sideways.
  const description = stage.errorDescription;

  return (
    <li className="grid grid-cols-[1rem_minmax(0,1fr)_auto] items-start gap-x-3 py-2.5 first:pt-0 last:pb-0">
      <span className="flex h-5 items-center">{stageIcon[state]}</span>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="font-medium text-sm">
          {name === undefined ? stage.name : i18n._(name)}
        </span>
        {details.length > 0 && (
          <span className="flex flex-wrap gap-x-3 gap-y-0.5 text-muted-foreground text-xs">
            {details}
          </span>
        )}
        {description !== undefined && (
          <p className="text-muted-foreground text-caption">
            <Trans id="admin.federation.diagnostics.stage.provider-message-line">
              Provider message:{" "}
              <span className="break-words">{description}</span>
            </Trans>
          </p>
        )}
      </div>
      <span
        // Only a failure is coloured: the icons already mark every outcome, and
        // a column of colour would bury the one stage that needs reading.
        className={`text-xs leading-5 ${
          state === "failed"
            ? "text-destructive-foreground"
            : "text-muted-foreground"
        }`}
      >
        {i18n._(stageStatusLabel(state))}
      </span>
    </li>
  );
}
