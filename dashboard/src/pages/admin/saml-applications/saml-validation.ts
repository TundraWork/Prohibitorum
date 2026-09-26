import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";

/**
 * The rules for the values a SAML application carries that the server does not
 * check.
 *
 * Both the create form and the detail page read these, so they live here rather
 * than in the page that needed them first: a rule written twice is a rule that
 * ends up disagreeing with itself. The client's check is the only one any of
 * them gets — a metadata document the server cannot parse comes back as a bare
 * bad request with no field named, and a malformed ACS address is stored as
 * given and then breaks every sign-in for the application.
 */

/** The two SAML bindings an Assertion Consumer Service endpoint may declare. */
export const bindingPost = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST";
export const bindingRedirect =
  "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect";

export const acsBindingInvalid = msg({
  id: "admin.saml-apps.acs.binding.invalid",
  message: "Choose the HTTP POST or the HTTP Redirect binding.",
});

export const acsAddressRequired = msg({
  id: "admin.saml-apps.acs.address.required",
  message: "Enter the Assertion Consumer Service address.",
});

export const acsAddressInvalid = msg({
  id: "admin.saml-apps.acs.address.invalid",
  message: "Use an absolute https address.",
});

export const acsIndexInvalid = msg({
  id: "admin.saml-apps.acs.index.invalid",
  message: "Use a whole number of zero or more.",
});

export const acsDefaultRequired = msg({
  id: "admin.saml-apps.acs.default.required",
  message: "Mark exactly one endpoint as the default.",
});

export const acsDefaultDuplicate = msg({
  id: "admin.saml-apps.acs.default.duplicate",
  message: "Only one endpoint can be the default one.",
});

export const acsRequired = msg({
  id: "admin.saml-apps.acs.required",
  message: "Add at least one Assertion Consumer Service endpoint.",
});

export const entityIdRequired = msg({
  id: "admin.saml-apps.entity-id.required",
  message: "Enter the service provider's Entity ID.",
});

export const sessionLifetimeInvalid = msg({
  id: "admin.saml-apps.lifetime.invalid",
  message: "Use a whole number of minutes, or leave it empty.",
});

export const displayNameRequired = msg({
  id: "admin.saml-apps.name.required",
  message: "Enter a name.",
});

export const metadataRequired = msg({
  id: "admin.saml-apps.metadata.required",
  message: "Paste the service provider's metadata document.",
});

export const metadataTooLarge = msg({
  id: "admin.saml-apps.metadata.too_large",
  message:
    "This document is too large to send. It and the rest of the form have to stay under 64 KiB.",
});

/**
 * One ACS endpoint while it is being edited. The index stays text: it is a
 * number on the wire, but a text input that holds `0` cannot be cleared to type
 * a different one, and an index is not a quantity to be stepped.
 */
export interface AcsRow {
  binding: string;
  location: string;
  index: string;
  isDefault: boolean;
}

/**
 * The whole request's ceiling, in UTF-8 bytes rather than characters.
 *
 * The body carries a metadata document, which is markup from somewhere else and
 * by far the largest thing any console form sends. Measuring here rather than
 * leaving it to the server's own body limit is for the reader's sake: a refusal
 * at that layer arrives as a bad request naming no field, and the document is
 * the only input that could have caused it.
 */
export const maxBodyBytes = 64 * 1024;

/**
 * The address an assertion is posted to.
 *
 * Only https is accepted: an assertion is a signed statement of who somebody
 * is, and the console should not be able to save an arrangement that sends one
 * over a channel anyone on the path can read. No trailing-slash rule is
 * imposed — the address is copied from a metadata document to the character,
 * and rewriting it would be the console's invention rather than the service
 * provider's statement.
 */
function addressProblem(value: string): MessageDescriptor | undefined {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return acsAddressInvalid;
  }
  if (url.protocol !== "https:") return acsAddressInvalid;
  if (url.host === "" || url.username !== "" || url.password !== "") {
    return acsAddressInvalid;
  }
  return undefined;
}

/**
 * Why the endpoint list is not usable, or undefined when it is.
 *
 * Every rule of the list is checked in one pass, and the first failure is what
 * the reader is told: a list is edited one row at a time, and a screen of
 * complaints about rows the reader has not reached yet is noise rather than
 * help. The row is named by its position in the message the caller draws, so
 * "Endpoint 2: …" points at the input that has to change.
 */
export function acsListProblem(
  rows: readonly AcsRow[],
): { row: number; message: MessageDescriptor } | undefined {
  if (rows.length === 0) return { row: 0, message: acsRequired };

  const firstDefault = rows.findIndex((row) => row.isDefault);
  if (firstDefault === -1) return { row: 0, message: acsDefaultRequired };
  // Reported on the later of the two, so the row the reader is most likely to
  // have just turned on is the one that carries the complaint.
  const secondDefault = rows.findIndex(
    (row, at) => row.isDefault && at > firstDefault,
  );
  if (secondDefault !== -1) {
    return { row: secondDefault, message: acsDefaultDuplicate };
  }

  for (const [index, row] of rows.entries()) {
    if (row.binding !== bindingPost && row.binding !== bindingRedirect) {
      return { row: index, message: acsBindingInvalid };
    }
    if (row.location === "") {
      return { row: index, message: acsAddressRequired };
    }
    const address = addressProblem(row.location);
    if (address !== undefined) return { row: index, message: address };
    if (!/^\d+$/.test(row.index.trim())) {
      return { row: index, message: acsIndexInvalid };
    }
  }
  return undefined;
}

/** The rows as the wire takes them: the index parsed back into a number. */
export function toAcsRequest(
  rows: readonly AcsRow[],
): { binding: string; location: string; index: number; isDefault: boolean }[] {
  return rows.map((row) => ({
    binding: row.binding,
    location: row.location,
    index: Number(row.index.trim()),
    isDefault: row.isDefault,
  }));
}
