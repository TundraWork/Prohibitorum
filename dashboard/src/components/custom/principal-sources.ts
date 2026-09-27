import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { PrincipalSource } from "@/api/raw-admin-paths";

/**
 * The account facts a downstream application may know someone by, and what
 * each one costs.
 *
 * The same three values are the OIDC subject source and the forward-auth
 * `Remote-User`, and the lists show them too, so their names are written once
 * here. The three differ in what the application sees, not in how the console
 * behaves: the account ID never changes, the username is readable but an
 * administrator can change it, and the verified email is readable but is
 * withheld — the request refused rather than answered with another identifier —
 * while the address is unverified or shared with another account. The
 * description says that, because the reader is choosing what their own
 * application keys its users on.
 */
const sub = {
  value: "sub",
  label: msg({ id: "principal-source.sub", message: "Account ID" }),
  description: msg({
    id: "principal-source.sub.hint",
    message: "Never changes.",
  }),
} as const;

const username = {
  value: "username",
  label: msg({ id: "principal-source.username", message: "Username" }),
  description: msg({
    id: "principal-source.username.hint",
    message: "Easy to read. An administrator can change it.",
  }),
} as const;

const verifiedEmail = {
  value: "verified_email",
  label: msg({
    id: "principal-source.verified-email",
    message: "Verified email",
  }),
  description: msg({
    id: "principal-source.verified-email.hint",
    message:
      "Sent only while the address is verified and no other account uses it; otherwise the request is refused.",
  }),
} as const;

export const principalSources: readonly {
  value: PrincipalSource;
  label: MessageDescriptor;
  description: MessageDescriptor;
}[] = [sub, username, verifiedEmail];

const labels: Record<PrincipalSource, MessageDescriptor> = {
  sub: sub.label,
  username: username.label,
  verified_email: verifiedEmail.label,
};

/** A source's name, for a sentence or a cell that mentions it. */
export function principalSourceLabel(
  source: PrincipalSource,
): MessageDescriptor {
  return labels[source];
}
