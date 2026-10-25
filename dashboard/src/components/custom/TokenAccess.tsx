import { Trans, useLingui } from "@lingui/react/macro";
import type { components } from "@/api/generated/schema";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Badge } from "@/components/ui/badge";

type Token = components["schemas"]["PersonalAccessTokenView"];

/**
 * What an access token may reach, as one line for a token's details.
 *
 * A token has one access level, and the levels include one another: the
 * applications chosen, every application, full access, and full access that
 * also skips confirming it is you. Only the first names anything, so it lists
 * the applications in the language of the page; the others say the level. The
 * last reads as full access here because `TokenAccessBadge` already marks what
 * sets it apart, and the row would otherwise say it twice.
 */
export function TokenAccessSummary({
  token,
}: {
  token: Pick<Token, "access" | "apps">;
}) {
  const { i18n } = useLingui();
  switch (token.access) {
    case "selected_apps": {
      const names = (token.apps ?? []).map((app) => app.displayName);
      if (names.length === 0) {
        return (
          <span className="text-muted-foreground">
            <Trans id="token.access.none">No applications</Trans>
          </span>
        );
      }
      return (
        <span className="wrap-anywhere">{formatNames(i18n.locale, names)}</span>
      );
    }
    case "all_apps":
      return <Trans id="token.access.all_apps">Every application</Trans>;
    case "full":
    case "sudo":
      return <Trans id="token.access.full">Full access</Trans>;
  }
}

/**
 * The application names as a list in the reader's language. Chinese joins
 * carry no space of their own, which run into a Latin name ("App 1和App 2"),
 * so a space is added where a joiner meets one.
 */
function formatNames(locale: string, names: string[]): string {
  const parts = new Intl.ListFormat(locale, {
    style: "short",
    type: "conjunction",
  }).formatToParts(names);
  if (!locale.startsWith("zh")) return parts.map((part) => part.value).join("");
  const latin = /[A-Za-z0-9]/;
  return parts
    .map((part, index) => {
      if (part.type !== "literal") return part.value;
      const before = parts[index - 1]?.value.at(-1) ?? "";
      const after = parts[index + 1]?.value.at(0) ?? "";
      return `${latin.test(before) ? " " : ""}${part.value}${latin.test(after) ? " " : ""}`;
    })
    .join("");
}

/**
 * The mark on a token that skips confirming it is you. Only that level gets
 * one: it is the one a reader should notice in a list, and the other levels
 * are the ordinary case.
 */
export function TokenAccessBadge({ token }: { token: Pick<Token, "access"> }) {
  if (token.access !== "sudo") return null;
  return (
    <Badge variant="warning">
      <Trans id="token.access.sudo.badge">Skips verifying your identity</Trans>
    </Badge>
  );
}

/**
 * When a token stops working: the day it expires, "Expired" once it has, or
 * "Never expires". The day only, since nobody plans around the hour.
 */
export function TokenExpiry({ token }: { token: Pick<Token, "expiresAt"> }) {
  const { i18n } = useLingui();
  if (token.expiresAt === undefined) {
    return <Trans id="token.expiry.never">Never expires</Trans>;
  }
  const expiry = new Date(token.expiresAt);
  if (expiry.getTime() <= Date.now()) {
    return (
      <Badge variant="destructive">
        <Trans id="token.expiry.expired">Expired</Trans>
      </Badge>
    );
  }
  const expires = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
  }).format(expiry);
  return <Trans id="token.expiry.on">Expires {expires}</Trans>;
}

/** When a token was last presented, or that it never has been. */
export function TokenLastUsed({ token }: { token: Pick<Token, "lastUsedAt"> }) {
  if (token.lastUsedAt === undefined) {
    return <Trans id="token.last-used.never">Not used yet</Trans>;
  }
  return (
    <Trans id="token.last-used.at">
      Last used <RelativeTime value={token.lastUsedAt} />
    </Trans>
  );
}
