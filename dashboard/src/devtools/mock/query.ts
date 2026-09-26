import {
  clampAdminCount,
  clampCount,
  clampDelay,
  defaultMockConfig,
  getMockConfig,
  updateMockConfig,
} from "@/devtools/mock/model";

/**
 * Drives the API mock from the URL search string, so a walkthrough or a review
 * pass can set up a screen without the devtools panel.
 *
 * `?mock` turns the mock on. Any other `mock.*` parameter also turns it on,
 * because asking for a specific row count and then having to say "and also run
 * it" would be a pointless second step.
 *
 * ## The vocabulary mirrors the config
 *
 * A parameter's name is the path to the field it sets — `mock.admin.oidcApps=7`
 * writes `config.admin.oidcApps` — rather than a second set of names invented
 * for URLs. Two names for one field drift the moment one of them is renamed, and
 * the panel already has to name every field it edits. The paths are validated
 * against `defaultMockConfig`, so a misspelled parameter is ignored rather than
 * creating a field nothing reads.
 *
 * Values are read the way the field's own type demands, and clamped the way the
 * panel clamps them: a count cannot exceed `mockAdminListMax`, a delay cannot
 * exceed `mockDelayMax`. A URL therefore cannot put the mock into a state the
 * panel could not reach.
 *
 * This is a development aid. It is wired in by `installApiMocks`, which only
 * runs under `import.meta.env.DEV`.
 */
export function applyMockQuery(search: string): string[] {
  const params = new URLSearchParams(search);
  const entries: [string, string][] = [];
  for (const [key, value] of params) {
    if (key === "mock" || key.startsWith("mock.")) entries.push([key, value]);
  }
  if (entries.length === 0) return [];

  // Applied to a copy first, then compared: the caller may be the router's
  // `onResolved`, and a write publishes to subscribers, which refresh the
  // queries and invalidate the router — which resolves again. A no-op for an
  // unchanged URL is what stops that from becoming an endless loop.
  const draft = structuredClone(getMockConfig()) as unknown as Record<
    string,
    unknown
  >;
  const applied: string[] = [];
  draft.enabled = true;
  for (const [key, value] of entries) {
    if (key === "mock") continue;
    const path = key.slice("mock.".length);
    if (!setPath(draft, path, value)) continue;
    applied.push(path);
  }

  const current = getMockConfig() as unknown as Record<string, unknown>;
  const changed =
    JSON.stringify(draft) !== JSON.stringify(current) ? draft : undefined;

  if (changed !== undefined) {
    updateMockConfig((target) => {
      Object.assign(target, changed);
    });
  }
  return applied;
}

/**
 * Writes one dotted path onto the draft, if the path names a real field and the
 * value fits that field's type. Returns whether it was applied, so the caller
 * can report what a URL actually did rather than assuming every parameter took.
 */
function setPath(
  draft: Record<string, unknown>,
  path: string,
  raw: string,
): boolean {
  const segments = path.split(".");
  const leaf = segments[segments.length - 1];
  if (leaf === undefined) return false;

  let target: Record<string, unknown> = draft;
  let shape: Record<string, unknown> = defaultMockConfig as unknown as Record<
    string,
    unknown
  >;
  for (const segment of segments.slice(0, -1)) {
    const next = target[segment];
    const nextShape = shape[segment];
    if (
      typeof next !== "object" ||
      next === null ||
      typeof nextShape !== "object" ||
      nextShape === null
    ) {
      return false;
    }
    target = next as Record<string, unknown>;
    shape = nextShape as Record<string, unknown>;
  }

  const current = target[leaf];
  const expected = shape[leaf];
  // The shape object is the authority on whether a name is real at all.
  if (expected === undefined) return false;

  if (typeof expected === "boolean") {
    const parsed = parseBoolean(raw);
    if (parsed === undefined) return false;
    target[leaf] = parsed;
    return true;
  }

  if (typeof expected === "number") {
    const parsed = Number(raw);
    if (!Number.isFinite(parsed)) return false;
    target[leaf] = clampFor(path, parsed);
    return true;
  }

  if (typeof expected === "string") {
    if (current === undefined) return false;
    const allowed = allowedValues(path);
    if (allowed !== undefined && !allowed.includes(raw)) return false;
    target[leaf] = raw;
    return true;
  }

  // A nested object reached as a leaf, an array, or a null default: nothing the
  // panel edits either, so a URL does not get to set it.
  return false;
}

/** Counts are clamped to the same ceilings the panel's own controls use. */
function clampFor(path: string, value: number): number {
  if (path === "delayMs") return clampDelay(value);
  if (path === "admin.diagnosticOutcome") return value;
  return path.startsWith("admin.") ? clampAdminCount(value) : clampCount(value);
}

/** Fields whose value is one of a fixed set, as the panel renders them. */
function allowedValues(path: string): readonly string[] | undefined {
  if (path === "session.role") return ["admin", "member"];
  if (path === "admin.diagnosticOutcome") return ["succeeded", "failed"];
  return undefined;
}

function parseBoolean(raw: string): boolean | undefined {
  if (raw === "" || raw === "1" || raw === "true") return true;
  if (raw === "0" || raw === "false") return false;
  return undefined;
}
