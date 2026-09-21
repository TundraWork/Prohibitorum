import { EventClient } from "@tanstack/devtools-event-client";
import type { SudoMethod } from "@/api/raw-paths";

/**
 * What the Prohibitorum panel watches: every step-up verification this tab
 * asked for, how it was answered, and whether the parked write went through.
 *
 * Keys are event suffixes only; `EventClient` prefixes the `pluginId`. Payloads
 * stay plain, serializable data because the same events also cross the Vite
 * server bus, which carries them as JSON.
 */
export type ProhibitorumDevtoolsEvents = {
  "sudo-challenge": { reason: string | null; stale: boolean };
  "sudo-granted": { method: "cached" | SudoMethod };
  "sudo-cancelled": undefined;
  "sudo-refused": { method: SudoMethod; reason: string };
};

export type ProhibitorumDevtoolsEventName = keyof ProhibitorumDevtoolsEvents;

type RecordedEvent<K extends ProhibitorumDevtoolsEventName> = {
  event: K;
  payload: ProhibitorumDevtoolsEvents[K];
  at: number;
};

/** One line of the panel: an event and when the browser received it. */
export type ProhibitorumDevtoolsRecord = {
  [K in ProhibitorumDevtoolsEventName]: RecordedEvent<K>;
}[ProhibitorumDevtoolsEventName];

class ProhibitorumDevtoolsClient extends EventClient<ProhibitorumDevtoolsEvents> {
  constructor() {
    super({ pluginId: "prohibitorum" });
  }
}

/** The single client every emitter and the panel share. */
export const prohibitorumDevtools = new ProhibitorumDevtoolsClient();

const historyLimit = 50;

// Subscribed at module scope rather than when the panel opens: the panel mounts
// on tab activation, so a listener registered there would miss everything the
// tab did before the user opened it.
let history: ReadonlyArray<ProhibitorumDevtoolsRecord> = [];
const subscribers = new Set<() => void>();

function record(entry: ProhibitorumDevtoolsRecord) {
  // Replaced, never mutated: `useSyncExternalStore` bails out while the
  // snapshot keeps its identity.
  history = [entry, ...history].slice(0, historyLimit);
  for (const notify of subscribers) notify();
}

prohibitorumDevtools.on("sudo-challenge", (event) => {
  record({ event: "sudo-challenge", payload: event.payload, at: Date.now() });
});
prohibitorumDevtools.on("sudo-granted", (event) => {
  record({ event: "sudo-granted", payload: event.payload, at: Date.now() });
});
prohibitorumDevtools.on("sudo-cancelled", () => {
  record({ event: "sudo-cancelled", payload: undefined, at: Date.now() });
});
prohibitorumDevtools.on("sudo-refused", (event) => {
  record({ event: "sudo-refused", payload: event.payload, at: Date.now() });
});

/** The recent step-up checks, newest first. */
export function getProhibitorumHistory(): ReadonlyArray<ProhibitorumDevtoolsRecord> {
  return history;
}

export function subscribeToProhibitorum(onChange: () => void): () => void {
  subscribers.add(onChange);
  return () => {
    subscribers.delete(onChange);
  };
}
