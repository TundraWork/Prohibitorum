import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { buildTotpUri, generateTotpSecret } from "@/api/auth";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";

/**
 * The locally generated authenticator secret and its otpauth URI.
 *
 * The secret never leaves the browser except as `secret_base32` on submit, and
 * a new one is drawn for every dialog that mounts this, so abandoning a
 * half-filled setup does not leave a stale secret to reappear.
 */
export function useTotpSetup() {
  const config = useQuery(publicConfigQueryOptions());
  const session = useQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  const [secret] = useState(() => generateTotpSecret());
  const username = session.data?.username;
  const totp = config.data?.totp;
  // buildTotpUri re-validates every field and throws on a malformed config, so
  // the two queries must both have landed before it is called.
  const uri =
    username !== undefined && totp !== undefined
      ? buildTotpUri(secret, username, totp)
      : "";
  return { secret, uri };
}
