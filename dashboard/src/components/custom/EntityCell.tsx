import { useLingui } from "@lingui/react/macro";
import { Link, type LinkProps } from "@tanstack/react-router";
import { Lock } from "lucide-react";
import type { ReactNode } from "react";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

/**
 * The leading cell of every management table: the entity's icon, its name, and
 * the short monospace identifier under it — a slug, a Client ID, an Entity ID.
 *
 * The name links to the entity's own page when there is one. A table whose rows
 * are all the same kind of record needs no column saying so, and the identifier
 * under the name is what an admin actually compares between rows.
 *
 * ## How a switched-off row reads
 *
 * A row that is disabled recedes — its icon and name drop a little in
 * opacity — and says so with a "Disabled" badge beside the name, which stays at
 * full strength. The dimming alone is not enough: a reader who cannot tell
 * opacities apart would have nothing, and a badge is also what a screen reader
 * finds. The identifier under the name is never dimmed: it is what a reader
 * scans for in a switched-off row, and faded muted text falls under the
 * contrast floor. The badge is small and secondary, so a list with a few
 * switched-off rows still reads as mostly settled ones.
 *
 * The text column is capped in width, so an identifier longer than the cap
 * ends in an ellipsis inside the visible area rather than running on under the
 * table's pinned column; its whole value is the tooltip and the record's page.
 *
 * `restricted` stays a padlock beside the name, because it says what a row *is*
 * rather than something that happened to it. `marks` is for a fact about the
 * row that wants words, such as a provider that cannot sign anyone in, drawn as
 * small badges after the disabled one.
 */
export function EntityCell({
  iconUrl,
  name,
  identifier,
  link,
  dimmed = false,
  restricted = false,
  marks,
  /** Overrides the avatar's placeholder; the app icon is the default. */
  fallback,
}: {
  iconUrl?: string | undefined;
  name: ReactNode;
  /** The monospace line under the name: a slug, Client ID or Entity ID. */
  identifier?: ReactNode;
  /** Where the name links; rendered as a plain name when omitted. */
  link?: LinkProps;
  /** The row is switched off; it recedes and says so to a screen reader. */
  dimmed?: boolean;
  /** The row is limited to selected user groups. */
  restricted?: boolean;
  /** Badges after the name, one per fact the row has to say in words. */
  marks?: ReactNode;
  fallback?: ReactNode;
}) {
  const { t } = useLingui();
  const restrictedLabel = t({
    id: "entity.restricted",
    message: "Available only to the selected user groups",
  });

  return (
    <div className="flex items-center gap-3">
      <span className={dimmed ? "opacity-70" : undefined}>
        <EntityAvatar
          iconUrl={iconUrl}
          name={typeof name === "string" ? name : undefined}
          fallback={fallback}
        />
      </span>
      <div className="flex max-w-52 min-w-0 flex-col sm:max-w-md">
        <span className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1">
          <span
            className={`min-w-0 truncate${dimmed ? " opacity-70" : ""}`}
            title={typeof name === "string" ? name : undefined}
          >
            {link === undefined ? (
              name
            ) : (
              <Link className="hover:underline" {...link}>
                {name}
              </Link>
            )}
          </span>
          {dimmed && (
            <Badge variant="secondary" size="sm">
              {t({ id: "entity.state.disabled", message: "Disabled" })}
            </Badge>
          )}
          {marks}
          {restricted && (
            <Tooltip>
              <TooltipTrigger
                delay={0}
                render={
                  <span
                    role="img"
                    aria-label={restrictedLabel}
                    className="inline-flex text-muted-foreground"
                  />
                }
              >
                <Lock size={14} aria-hidden="true" />
              </TooltipTrigger>
              <TooltipPopup>{restrictedLabel}</TooltipPopup>
            </Tooltip>
          )}
        </span>
        {identifier !== undefined && (
          <span
            title={typeof identifier === "string" ? identifier : undefined}
            className="truncate font-mono text-muted-foreground text-xs"
          >
            {identifier}
          </span>
        )}
      </div>
    </div>
  );
}
