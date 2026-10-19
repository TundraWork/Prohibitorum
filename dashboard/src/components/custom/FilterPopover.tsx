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
 * The filters a list keeps out of its toolbar row: one outline button that says
 * how many are set, and a popover holding their controls.
 *
 * The search stays in the row because it is typed all the time; what needs
 * several controls to say — a provider, a field, an operator and a value — goes
 * here, so the row stays one line and wraps cleanly on a narrow screen. `count`
 * is the number of filters set inside the popover, not the whole list's, so the
 * chip answers "is something hidden in here?".
 */
export function FilterPopover({
  count,
  children,
}: {
  count: number;
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
      <PopoverPopup align="end" className="w-80">
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
