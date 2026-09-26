import { Tooltip } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { Lock } from "lucide-react";
import type { ReactNode } from "react";
import { EntityAvatar } from "@/components/custom/EntityAvatar";

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
 * A row that is disabled recedes: the whole cell drops to a fraction of its
 * opacity, icon and text together. It is not labelled — a chip reading
 * "Disabled" on every settled row costs the measure and buries the rows that are
 * genuinely wrong, and there is no second state to tell it apart from, so the
 * words would carry nothing the recession does not.
 *
 * The recession is decoration on top of a fact, never the only record of it: an
 * `sr-only` line follows the name, because a screen reader sees no opacity, and
 * a reader who cannot distinguish the dimming would otherwise have no way to
 * know the row is switched off. The state itself is on the record's own page,
 * where it can be changed.
 *
 * `restricted` stays a padlock beside the name, because it says what a row *is*
 * rather than something that happened to it.
 */
export function EntityCell({
  iconUrl,
  name,
  identifier,
  href,
  dimmed = false,
  restricted = false,
  /** Overrides the avatar's placeholder; the app icon is the default. */
  fallback,
}: {
  iconUrl?: string | undefined;
  name: ReactNode;
  /** The monospace line under the name: a slug, Client ID or Entity ID. */
  identifier?: ReactNode;
  /** Where the name links; rendered as a plain name when omitted. */
  href?: string;
  /** The row is switched off; it recedes and says so to a screen reader. */
  dimmed?: boolean;
  /** The row is limited to selected user groups. */
  restricted?: boolean;
  fallback?: ReactNode;
}) {
  const { t } = useLingui();

  return (
    <div className={`flex items-center gap-3 ${dimmed ? "opacity-60" : ""}`}>
      <EntityAvatar iconUrl={iconUrl} fallback={fallback} />
      <div className="flex min-w-0 flex-col">
        <span className="flex items-center gap-1.5">
          {href === undefined ? (
            name
          ) : (
            <a className="hover:underline" href={href}>
              {name}
            </a>
          )}
          {dimmed && (
            <span className="sr-only">
              {t({ id: "entity.state.disabled", message: "Disabled" })}
            </span>
          )}
          {restricted && (
            <Tooltip delay={0}>
              <Tooltip.Trigger>
                <span className="inline-flex text-muted">
                  <Lock
                    size={14}
                    aria-hidden="true"
                    aria-label={t({
                      id: "entity.restricted",
                      message: "Available only to the selected user groups",
                    })}
                  />
                </span>
              </Tooltip.Trigger>
              <Tooltip.Content>
                {t({
                  id: "entity.restricted",
                  message: "Available only to the selected user groups",
                })}
              </Tooltip.Content>
            </Tooltip>
          )}
        </span>
        {identifier !== undefined && (
          <span className="truncate font-mono text-xs text-muted">
            {identifier}
          </span>
        )}
      </div>
    </div>
  );
}
