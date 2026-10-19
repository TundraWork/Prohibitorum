import { Trans, useLingui } from "@lingui/react/macro";
import { ListFilterIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverPopup,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";

/**
 * The filters a list keeps out of its toolbar row: a button that says how many
 * are set, and a popover holding their controls.
 *
 * `count` is the number set inside the popover, so the chip answers "is
 * something hidden in here?"; the search stays in the row.
 */
export function FilterPopover({
  count,
  align = "end",
  children,
}: {
  count: number;
  /** The popover's edge that lines up with the button: pick the one that keeps it in the page column. */
  align?: "start" | "end";
  children: ReactNode;
}) {
  const { t } = useLingui();
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            aria-label={
              count === 0
                ? t({ id: "filters.button", message: "Filters" })
                : t({
                    id: "filters.button.active",
                    message: `Filters, ${count} active`,
                  })
            }
          />
        }
      >
        <ListFilterIcon aria-hidden="true" />
        <Trans id="filters.button">Filters</Trans>
        {count > 0 && (
          <Badge variant="info" size="sm" aria-hidden="true">
            {count}
          </Badge>
        )}
      </PopoverTrigger>
      <PopoverPopup align={align} collisionPadding={24} className="w-80">
        <div className="flex w-full flex-col gap-4">
          <PopoverTitle className="text-base">
            <Trans id="filters.title">Filters</Trans>
          </PopoverTitle>
          {children}
        </div>
      </PopoverPopup>
    </Popover>
  );
}
