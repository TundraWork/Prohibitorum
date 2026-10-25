import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { providerProtocolLabel } from "@/pages/admin/identity-providers/provider-options";

/**
 * The server describes each node of an explanation with a short key: a
 * combinator (`all`, `any`, `not`) or `fact=value`. These are the sentences the
 * console writes for the keys it knows.
 */

type Translate = (descriptor: MessageDescriptor) => string;

const combinatorSentences: Readonly<Record<string, MessageDescriptor>> = {
  all: msg({
    id: "admin.group.explain.all",
    message: "Every condition below applies",
  }),
  any: msg({
    id: "admin.group.explain.any",
    message: "At least one condition below applies",
  }),
  not: msg({
    id: "admin.group.explain.not",
    message: "The condition below does not apply",
  }),
};

const factSentences: Readonly<Record<string, MessageDescriptor>> = {
  "login_method=passkey": msg({
    id: "admin.group.explain.method.passkey",
    message: "Signs in with a passkey",
  }),
  "login_method=password_totp": msg({
    id: "admin.group.explain.method.password_totp",
    message: "Signs in with a password and authenticator",
  }),
  "login_method=federation": msg({
    id: "admin.group.explain.method.federation",
    message: "Signs in through a linked provider",
  }),
  "avatar=any": msg({
    id: "admin.group.explain.avatar.any",
    message: "Has a profile picture",
  }),
  "avatar=user_uploaded": msg({
    id: "admin.group.explain.avatar.user_uploaded",
    message: "Uploaded their own profile picture",
  }),
};

const protocolSentence = msg({
  id: "admin.group.explain.protocol",
  message: "Is linked through {protocol}",
});

/**
 * What an explanation label says: a finished sentence, or a provider the
 * caller draws inside its own sentence: by its name, kept whole across a line
 * break, or, when the catalogue does not know it, by its slug as an identifier.
 */
export type ExplainSentence =
  | { text: string }
  | { provider: string }
  | { unknownProvider: string };

/**
 * The sentence for one explanation label, or `undefined` for a key the console
 * has no words for, which the caller draws as it arrived. `providerName` turns
 * a provider's slug into its name; a slug it does not know comes back as
 * `unknownProvider`.
 */
export function explainSentence(
  label: string,
  t: Translate,
  providerName: (slug: string) => string | undefined,
): ExplainSentence | undefined {
  const combinator = combinatorSentences[label];
  if (combinator !== undefined) return { text: t(combinator) };
  const fact = factSentences[label];
  if (fact !== undefined) return { text: t(fact) };
  const separator = label.indexOf("=");
  if (separator < 0) return undefined;
  const key = label.slice(0, separator);
  const value = label.slice(separator + 1);
  if (key === "connection.provider") {
    const provider = providerName(value);
    if (provider === undefined) return { unknownProvider: value };
    return { provider };
  }
  if (
    key === "connection.protocol" &&
    (value === "oidc" || value === "steam" || value === "vrchat")
  ) {
    const protocol = t(providerProtocolLabel(value));
    return { text: t({ ...protocolSentence, values: { protocol } }) };
  }
  return undefined;
}
