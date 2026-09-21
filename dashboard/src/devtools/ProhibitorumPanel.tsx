import { useSyncExternalStore } from "react";
import {
  getProhibitorumHistory,
  type ProhibitorumDevtoolsRecord,
  subscribeToProhibitorum,
} from "@/devtools/events";

type Tone = "neutral" | "positive" | "warning" | "negative";

const accent: Record<Tone, string> = {
  neutral: "border-l-slate-400",
  positive: "border-l-emerald-500",
  warning: "border-l-amber-500",
  negative: "border-l-rose-500",
};

const label: Record<Tone, string> = {
  neutral: "",
  positive: "text-emerald-500",
  warning: "text-amber-500",
  negative: "text-rose-500",
};

function summarize(record: ProhibitorumDevtoolsRecord): {
  title: string;
  detail: string;
  tone: Tone;
} {
  switch (record.event) {
    case "sudo-challenge":
      return {
        title: "Verification required",
        detail: record.payload.stale
          ? "the cached window had lapsed"
          : "no window was open",
        tone: "warning",
      };
    case "sudo-granted":
      return {
        title: "Operation replayed",
        detail:
          record.payload.method === "cached"
            ? "signed in recently enough to skip the prompt"
            : `verified with ${record.payload.method}`,
        tone: "positive",
      };
    case "sudo-cancelled":
      return {
        title: "Prompt dismissed",
        detail: "the operation was abandoned",
        tone: "neutral",
      };
    case "sudo-refused":
      return {
        title: "Verification refused",
        detail: `${record.payload.method} · ${record.payload.reason}`,
        tone: "negative",
      };
  }
}

function clock(at: number): string {
  return new Date(at).toLocaleTimeString(undefined, { hour12: false });
}

/**
 * The product panel: a running account of the step-up checks the console asked
 * for, so a guarded write that prompted when it should not have — or one that
 * slid through on a stale window — is visible without reading the network log.
 */
export function ProhibitorumPanel() {
  const history = useSyncExternalStore(
    subscribeToProhibitorum,
    getProhibitorumHistory,
    getProhibitorumHistory,
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-3 text-sm">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="font-medium">Step-up checks</h2>
        <span className="text-xs opacity-60">
          {history.length === 0 ? "nothing yet" : `last ${history.length}`}
        </span>
      </div>
      {history.length === 0 ? (
        <p className="text-xs opacity-60">
          A write that needs your identity confirmed again shows up here.
        </p>
      ) : (
        <ol className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto">
          {history.map((record) => {
            const summary = summarize(record);
            return (
              <li
                key={`${record.at}-${record.event}`}
                className={`flex gap-3 border-l-2 py-1.5 pl-3 ${accent[summary.tone]}`}
              >
                <time className="shrink-0 font-mono text-xs tabular-nums opacity-60">
                  {clock(record.at)}
                </time>
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className={`font-medium ${label[summary.tone]}`}>
                    {summary.title}
                  </span>
                  <span className="text-xs break-words opacity-70">
                    {summary.detail}
                  </span>
                </span>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
