import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";

/**
 * The "one value per line" fields: redirect URIs, scopes, allowed e-mail
 * domains, trusted proxies, SAML ACS addresses.
 *
 * They all share the same reading, and it is deliberately not forgiving. A
 * blank line only separates groups and is dropped, a line with spaces around it
 * is an error rather than something to trim, and any other complaint names the
 * line it is on — numbered from 1, as the reader counts them. Trimming would
 * mean saving something other than what was typed, and a redirect URI or a scope
 * name that is nearly right is a sign-in failure later rather than a typo now.
 *
 * The rule is written once here so every such field behaves the same way, and so
 * the line numbering cannot drift between them.
 */

export const lineMessages = {
  /** Takes `{ line }`. */
  spaces: msg({
    id: "form.lines.spaces",
    message: "Line {line}: remove the spaces at either end.",
  }),
  /** Takes `{ line }`. */
  invalid: msg({
    id: "form.lines.invalid",
    message: "Line {line}: {reason}",
  }),
} as const;

/**
 * What is wrong with one line, with the line number the reader sees. `reason`
 * is the field's own complaint about that line's contents.
 */
export interface LineProblem {
  line: number;
  reason: MessageDescriptor;
}

/**
 * One line's value, or why it is not usable. Returning the reason rather than a
 * boolean lets a field say what is wrong — a scope name's characters, an
 * address's shape — instead of one message for every failure.
 */
export type LineCheck = (value: string) => MessageDescriptor | undefined;

export type LinesParse<T> =
  | { ok: true; values: T[] }
  | { ok: false; problem: LineProblem };

/**
 * Reads a one-per-line box.
 *
 * `check` says whether one line's text is usable; a line it rejects is reported
 * with its number and the reason. An empty box is `ok` with no values — whether
 * that is allowed is the field's own rule, since "at least one redirect URI" and
 * "no allowed domains" are both legitimate.
 */
export function parseLines(text: string, check: LineCheck): LinesParse<string> {
  const values: string[] = [];
  const lines = text.split("\n");
  for (const [index, raw] of lines.entries()) {
    // A box pasted from Windows carries `\r\n`; the `\r` is the line break, not
    // part of the value.
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    if (line.trim() === "") continue;
    if (line !== line.trim()) {
      return {
        ok: false,
        problem: { line: index + 1, reason: lineMessages.spaces },
      };
    }
    const reason = check(line);
    if (reason !== undefined) {
      return { ok: false, problem: { line: index + 1, reason } };
    }
    values.push(line);
  }
  return { ok: true, values };
}

/**
 * The same parse, but the lines are mapped to a value of the caller's own type
 * once they pass. For a scope vocabulary or an ACS entry, the line is not the
 * value that gets stored.
 */
export function parseLinesAs<T>(
  text: string,
  parseLine: (
    value: string,
  ) => { value: T; reason?: undefined } | { reason: MessageDescriptor },
): LinesParse<T> {
  const values: T[] = [];
  const lines = text.split("\n");
  for (const [index, raw] of lines.entries()) {
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    if (line.trim() === "") continue;
    if (line !== line.trim()) {
      return {
        ok: false,
        problem: { line: index + 1, reason: lineMessages.spaces },
      };
    }
    const parsed = parseLine(line);
    if (parsed.reason !== undefined) {
      return { ok: false, problem: { line: index + 1, reason: parsed.reason } };
    }
    values.push(parsed.value);
  }
  return { ok: true, values };
}

/** The values as the box shows them, one per line. */
export function formatLines(values: readonly string[]): string {
  return values.join("\n");
}

/**
 * A line problem as a message the reader gets. The line number is carried as a
 * placeholder rather than baked into the text, the way every other numbered
 * message on the console is written, so the translators keep one string and the
 * number is formatted in the reader's locale.
 */
export function lineProblemMessage(problem: LineProblem): MessageDescriptor {
  return {
    ...problem.reason,
    values: { line: problem.line },
  } as MessageDescriptor;
}
