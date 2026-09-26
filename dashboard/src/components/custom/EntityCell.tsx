import { Badge, Tooltip } from "@heroui/react";
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
 * ## State rides on the icon
 *
 * These tables do not carry a status column. A column of "Enabled / Enabled /
 * Disabled / Enabled" is mostly the word "Enabled" — it costs a column and the
 * reader still has to scan it. Instead a row that is *not* ordinary gets a dot
 * on its icon, and a row that is fine gets nothing at all, so the exceptions are
 * what the eye lands on.
 *
 * The dot is never the only carrier of its meaning: the icon has a tooltip, and
 * a screen-reader-only copy of the same words follows the name, so the state
 * reaches the keyboard and a reader who cannot see the colour.
 *
 * `restricted` is a padlock rather than a dot, because it says what a row *is*
 * rather than something that just happened to it, and it can be true at the same
 * time as a dot without the two fighting for the same corner.
 */
export type EntityState = "disabled" | "not_ready";

export function EntityCell({
  iconUrl,
  name,
  identifier,
  href,
  state,
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
  /** Why this row is not ordinary. Only pass it when that is true. */
  state?: EntityState;
  /** The row is limited to selected user groups. */
  restricted?: boolean;
  fallback?: ReactNode;
}) {
  const { t } = useLingui();
  const stateLabel =
    state === "disabled"
      ? t({ id: "entity.state.disabled", message: "Disabled" })
      : state === "not_ready"
        ? t({ id: "entity.state.not_ready", message: "Not ready" })
        : undefined;

  const avatar = <EntityAvatar iconUrl={iconUrl} fallback={fallback} />;

  return (
    <div className="flex items-center gap-3">
      {stateLabel === undefined ? (
        avatar
      ) : (
        // `Badge.Anchor` is the positioning wrapper and holds both the avatar
        // and the badge; the badge is absolutely placed against it, so the dot
        // lands on the avatar's corner and reads as the entity's state rather
        // than as a list marker. `Badge` with no children draws just the dot.
        // The nesting is the library's own and is not interchangeable: with
        // `Badge` outside, the anchor would be the positioned box and the
        // avatar, taken out of flow, would disappear with the dot.
        <Badge.Anchor>
          {avatar}
          <Badge
            color={state === "disabled" ? "default" : "warning"}
            placement="bottom-right"
            size="sm"
            aria-label={stateLabel}
          />
        </Badge.Anchor>
      )}
      <div className="flex min-w-0 flex-col">
        <span
          className={`flex items-center gap-1.5 ${state === "disabled" ? "text-muted" : ""}`}
        >
          {href === undefined ? (
            name
          ) : (
            <a className="hover:underline" href={href}>
              {name}
            </a>
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
          {stateLabel !== undefined && (
            <span className="sr-only">{stateLabel}</span>
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
