import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { RuleFact } from "@/pages/admin/groups/rule-model";
import { providerProtocolLabel } from "@/pages/admin/identity-providers/provider-options";

/** The words the rule builder uses for facts, values and server reason codes. */

export const factLabels: Readonly<Record<RuleFact, MessageDescriptor>> = {
  login_method: msg({
    id: "admin.group.rule.fact.login_method",
    message: "Signs in with",
  }),
  "connection.provider": msg({
    id: "admin.group.rule.fact.provider",
    message: "Linked to",
  }),
  "connection.protocol": msg({
    id: "admin.group.rule.fact.protocol",
    message: "Linked through",
  }),
  avatar: msg({
    id: "admin.group.rule.fact.avatar",
    message: "Profile picture",
  }),
};

const methodLabels: Readonly<Record<string, MessageDescriptor>> = {
  passkey: msg({
    id: "admin.group.rule.method.passkey",
    message: "A passkey",
  }),
  password_totp: msg({
    id: "admin.group.rule.method.password_totp",
    message: "Password and authenticator",
  }),
  federation: msg({
    id: "admin.group.rule.method.federation",
    message: "A linked provider",
  }),
};

const sourceLabels: Readonly<Record<string, MessageDescriptor>> = {
  any: msg({ id: "admin.group.rule.source.any", message: "Any picture" }),
  user_uploaded: msg({
    id: "admin.group.rule.source.user_uploaded",
    message: "Uploaded by the user",
  }),
};

/**
 * A fixed value's name, or `undefined` for a value the fact does not take, which
 * the caller draws as it arrived. A provider's name comes from the catalogue.
 */
export function valueLabel(
  fact: RuleFact,
  value: string,
): MessageDescriptor | undefined {
  if (fact === "login_method") return methodLabels[value];
  if (fact === "avatar") return sourceLabels[value];
  if (fact === "connection.protocol") {
    return value === "oidc" || value === "steam" || value === "vrchat"
      ? providerProtocolLabel(value)
      : undefined;
  }
  return undefined;
}

/** The wording of a rule problem on its own row, by server reason code. */
export const issueMessages: Readonly<Record<string, MessageDescriptor>> = {
  empty_rule: msg({
    id: "admin.group.rule.empty",
    message: "A rule needs at least one condition.",
  }),
  invalid_shape: msg({
    id: "admin.group.rule.reason.invalid_shape",
    message: "This condition combines fields that cannot be used together.",
  }),
  invalid_op: msg({
    id: "admin.group.rule.reason.invalid_op",
    message: "That group type is not supported.",
  }),
  invalid_fact: msg({
    id: "admin.group.rule.reason.invalid_fact",
    message: "That condition type is not supported.",
  }),
  max_depth_exceeded: msg({
    id: "admin.group.rule.reason.max_depth_exceeded",
    message: "Conditions are nested too deeply. Use at most 8 levels.",
  }),
  max_nodes_exceeded: msg({
    id: "admin.group.rule.reason.max_nodes_exceeded",
    message: "A rule can contain at most 64 conditions in total.",
  }),
  max_children_exceeded: msg({
    id: "admin.group.rule.reason.max_children_exceeded",
    message: "Each group can contain at most 32 conditions.",
  }),
  empty_children: msg({
    id: "admin.group.rule.reason.empty_children",
    message: "A group needs at least one condition.",
  }),
  missing_children: msg({
    id: "admin.group.rule.reason.missing_children",
    message: "A group needs at least one condition.",
  }),
  not_requires_fact: msg({
    id: "admin.group.rule.reason.not_requires_fact",
    message: "A negation must contain a single condition, not a group.",
  }),
  missing_provider: msg({
    id: "admin.group.rule.reason.missing_provider",
    message: "Choose a provider for this condition.",
  }),
  provider_not_found: msg({
    id: "admin.group.rule.reason.provider_not_found",
    message: "That provider is not available on this instance.",
  }),
  invalid_protocol: msg({
    id: "admin.group.rule.reason.invalid_protocol",
    message: "That protocol is not supported.",
  }),
  invalid_method: msg({
    id: "admin.group.rule.reason.invalid_method",
    message: "That sign-in method is not supported.",
  }),
  invalid_source: msg({
    id: "admin.group.rule.reason.invalid_source",
    message: "That picture source is not supported.",
  }),
  unsupported_version: msg({
    id: "admin.group.rule.reason.unsupported_version",
    message: "This rule was written for a different server version.",
  }),
  invalid_json: msg({
    id: "admin.group.rule.reason.invalid_json",
    message: "The document is not valid JSON.",
  }),
  unknown_field: msg({
    id: "admin.group.rule.reason.unknown_field",
    message: "The document has a field the server does not recognise.",
  }),
  trailing_json: msg({
    id: "admin.group.rule.reason.trailing_json",
    message: "The document has extra content after the rule.",
  }),
  missing_version: msg({
    id: "admin.group.rule.reason.missing_version",
    message: "The document is missing its version.",
  }),
  missing_condition: msg({
    id: "admin.group.rule.reason.missing_condition",
    message: "The document is missing its condition.",
  }),
};

/** For a refusal with no reason the builder has words for, and for a preview that failed. */
export const ruleRejected = msg({
  id: "admin.group.rule.rejected",
  message: "The server did not accept this rule.",
});

/** The wording for a reason code; a code nobody wrote words for reads as a plain refusal. */
export function issueMessage(code: string): MessageDescriptor {
  return issueMessages[code] ?? ruleRejected;
}
