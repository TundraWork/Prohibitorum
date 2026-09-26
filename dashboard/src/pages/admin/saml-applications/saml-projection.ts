import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { SamlAttributeMapping } from "@/api/raw-admin-paths";

/**
 * What the SAML sections know about identity projection: the NameID formats an
 * administrator may choose from, the account facts an attribute may read, and
 * the checks the server does not make.
 *
 * The server stores an attribute map as it receives it and validates none of it
 * (`handle_admin_saml_sps.go` writes the decoded JSON straight to the column),
 * so a mapping that names a claim the instance never emits, or names two
 * mappings the same thing, is accepted here and only shows up as a sign-in that
 * comes back without the attribute. The checks in this file are therefore the
 * only ones between a typo and that failure.
 *
 * ## Why the checks live outside the form
 *
 * A row's complaint belongs to a row and a field, not to the map as a whole, and
 * working out which row is at fault means reading the whole value. Keeping that
 * here — rather than inside the section's `onSubmit` — lets the rule be read and
 * tested on its own, and keeps the same wording available to the create page,
 * which sends no map at all but does share the format list.
 */

/** The four formats the specification names, in the order SAML 2.0 Core lists them. */
export const nameIdFormats = [
  "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
  "urn:oasis:names:tc:SAML:2.0:nameid-format:transient",
  "urn:oasis:names:tc:SAML:2.0:nameid-format:emailAddress",
  "urn:oasis:names:tc:SAML:2.0:nameid-format:unspecified",
] as const;

/**
 * The account facts a mapping may read. `attributes.<key>` is the fourth and
 * needs a key beside it, so it is handled separately from the three named ones.
 */
export const attributeSources = ["username", "avatar_url", "groups"] as const;

export const attributeSourcePrefix = "attributes.";

/** Every source a mapping may carry, named ones and account attributes alike. */
export function isKnownAttributeSource(source: string): boolean {
  if (source.startsWith(attributeSourcePrefix)) {
    return source.length > attributeSourcePrefix.length;
  }
  return (attributeSources as readonly string[]).includes(source);
}

const sourceMessages = {
  username: msg({ id: "admin.saml-apps.source.username", message: "Username" }),
  avatarUrl: msg({
    id: "admin.saml-apps.source.avatar-url",
    message: "Avatar URL",
  }),
  groups: msg({ id: "admin.saml-apps.source.groups", message: "User groups" }),
  attributes: msg({
    id: "admin.saml-apps.source.attributes",
    message: "Account attribute",
  }),
} as const;

export function attributeSourceLabel(source: string): MessageDescriptor {
  if (source.startsWith(attributeSourcePrefix)) {
    return sourceMessages.attributes;
  }
  if (source === "username") return sourceMessages.username;
  if (source === "avatar_url") return sourceMessages.avatarUrl;
  if (source === "groups") return sourceMessages.groups;
  return sourceMessages.attributes;
}

/** The fourth choice's label on its own, without a source string to pass. */
export const accountAttributeLabel: MessageDescriptor =
  sourceMessages.attributes;

export const mappingMessages = {
  nameRequired: msg({
    id: "admin.saml-apps.mapping.name.required",
    message: "Give this attribute a name.",
  }),
  nameDuplicate: msg({
    id: "admin.saml-apps.mapping.name.duplicate",
    message: "Another attribute already sends under this name.",
  }),
  sourceUnknown: msg({
    id: "admin.saml-apps.mapping.source.unknown",
    message: "Choose where this attribute reads from.",
  }),
  keyRequired: msg({
    id: "admin.saml-apps.mapping.key.required",
    message: "Name the account attribute this reads from.",
  }),
} as const;

/**
 * What is wrong with one row, keyed by the input that should carry it. The
 * section turns these into `RowsField` row problems so each complaint draws
 * under the input it belongs to rather than under the whole list.
 */
export interface MappingProblem {
  index: number;
  field: "name" | "source" | "key";
  message: MessageDescriptor;
}

/**
 * Checks the whole map: names that are present and unique, sources from the
 * known set, and a key whenever the source is an account attribute.
 *
 * Duplicate names are reported on every row that repeats rather than only the
 * later ones: which of the two is the mistake is not something the console can
 * know, and a reader who is told about one of them would fix it and be told
 * about the other.
 */
export function attributeMapProblems(
  mappings: readonly SamlAttributeMapping[],
): MappingProblem[] {
  const counts = new Map<string, number>();
  for (const mapping of mappings) {
    const name = mapping.name.trim();
    if (name === "") continue;
    counts.set(name, (counts.get(name) ?? 0) + 1);
  }

  const problems: MappingProblem[] = [];
  for (const [index, mapping] of mappings.entries()) {
    const name = mapping.name.trim();
    if (name === "") {
      problems.push({
        index,
        field: "name",
        message: mappingMessages.nameRequired,
      });
    } else if ((counts.get(name) ?? 0) > 1) {
      problems.push({
        index,
        field: "name",
        message: mappingMessages.nameDuplicate,
      });
    }

    const source = mapping.source.trim();
    if (!isKnownAttributeSource(source)) {
      problems.push({
        index,
        field: "source",
        message: mappingMessages.sourceUnknown,
      });
      // The key only means anything once a source is chosen, so a row without
      // one is not also told its key is missing.
      continue;
    }
    if (
      source.startsWith(attributeSourcePrefix) &&
      source.slice(attributeSourcePrefix.length).trim() === ""
    ) {
      problems.push({
        index,
        field: "key",
        message: mappingMessages.keyRequired,
      });
    }
  }
  return problems;
}

/** The key inside an `attributes.<key>` source, or an empty string. */
export function attributeKeyOf(source: string): string {
  return source.startsWith(attributeSourcePrefix)
    ? source.slice(attributeSourcePrefix.length)
    : "";
}

/**
 * The last segment of a NameID format URN: `persistent` rather than
 * `urn:oasis:names:tc:SAML:2.0:nameid-format:persistent`.
 *
 * The server stores and returns the whole URN and so does the console pass it
 * back unchanged — only the reading is shortened, which is why this lives beside
 * the format list that the shortened names also label.
 */
export function shortNameIdFormat(format: string): string {
  const separator = format.lastIndexOf(":");
  const short = separator === -1 ? format : format.slice(separator + 1);
  return short === "" ? format : short;
}

/** The source an attribute key composes to, with the key's own spaces removed. */
export function attributeSourceOf(key: string): string {
  return `${attributeSourcePrefix}${key.trim()}`;
}

/**
 * The `NameFormat` a new mapping starts with.
 *
 * SAML 2.0 Core's `basic`, which is what the instance's own GHES profile uses
 * and what a service provider reading an attribute name as a plain string
 * expects. A provider that requires `uri` instead types it — the wire field is
 * free text, and this is a starting value rather than a closed list.
 */
export const defaultAttributeNameFormat =
  "urn:oasis:names:tc:SAML:2.0:attrname-format:basic";
