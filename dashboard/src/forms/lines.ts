import type { MessageDescriptor } from "@lingui/core";

/**
 * A row-level problem in a `RowsField`: which row, numbered from 1 as the
 * reader counts them, and what is wrong with it.
 *
 * The row editors — a forward-auth scope vocabulary, an OIDC claim alias table,
 * a SAML attribute map and ACS list — all report a mistake on the row it is in
 * rather than on the field as a whole, and the number is written once here so
 * no two of them count differently. `reason` is the editor's own complaint
 * about that row's contents.
 */
export interface LineProblem {
  line: number;
  reason: MessageDescriptor;
}

/**
 * A row problem as a message the reader gets. The row number is carried as a
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
