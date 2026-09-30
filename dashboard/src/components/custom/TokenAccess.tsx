import { Chip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import type { components } from "@/api/generated/schema";

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
          <span className="text-muted">
            <Trans id="token.access.none">No applications</Trans>
          </span>
        );
      }
      return (
        <span className="wrap-anywhere">
          {new Intl.ListFormat(i18n.locale, {
            style: "short",
            type: "conjunction",
          }).format(names)}
        </span>
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
 * The mark on a token that skips confirming it is you. Only that level gets
 * one: it is the one a reader should notice in a list, and the other levels
 * are the ordinary case.
 */
export function TokenAccessBadge({ token }: { token: Pick<Token, "access"> }) {
  if (token.access !== "sudo") return null;
  return (
    <Chip color="warning" size="sm" variant="soft">
      <Trans id="token.access.sudo.badge">Skips confirming it is you</Trans>
    </Chip>
  );
}
