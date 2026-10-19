import type { DateRange } from "@daypicker/react";
import { enUS, zhCN } from "@daypicker/react/locale";
import { Trans, useLingui } from "@lingui/react/macro";
import { CalendarRange } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/custom/Button";
import { Button as PlainButton } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  Popover,
  PopoverPopup,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { parseDay } from "@/pages/admin/logs/audit-filters";

/** A calendar day as the address writes it, in the reader's own time zone. */
function dayText(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

/**
 * The days of a custom range, as one button beside the range select and a
 * calendar behind it.
 *
 * It reports a range only once both ends are chosen; until then the address
 * keeps what it had and the page says which part is missing. Clearing reports
 * no days at all.
 */
export function AuditDateRange({
  from,
  to,
  onChange,
}: {
  from: string;
  to: string;
  onChange: (from: string, to: string) => void;
}) {
  const { t, i18n } = useLingui();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<DateRange | undefined>(undefined);
  const start = parseDay(from);
  const end = parseDay(to);
  const saved =
    start !== null && end !== null ? { from: start, to: end } : undefined;

  const day = new Intl.DateTimeFormat(i18n.locale, { dateStyle: "medium" });
  const summary =
    saved === undefined
      ? t({ id: "admin.logs.range.choose", message: "Choose dates" })
      : // Each end on its own, so a reversed range reads as it was entered.
        `${day.format(saved.from)} – ${day.format(saved.to)}`;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        // Each opening starts from what is saved, so half a range never lingers.
        if (next) setDraft(saved);
        setOpen(next);
      }}
    >
      <PopoverTrigger render={<PlainButton variant="outline" />}>
        <CalendarRange aria-hidden="true" />
        {summary}
      </PopoverTrigger>
      <PopoverPopup align="start">
        <div className="flex flex-col gap-2">
          <PopoverTitle className="sr-only">
            <Trans id="admin.logs.range.dates">Dates</Trans>
          </PopoverTitle>
          <Calendar
            mode="range"
            resetOnSelect
            // A day that has not happened can only give an empty range.
            disabled={{ after: new Date() }}
            locale={i18n.locale === "zh" ? zhCN : enUS}
            defaultMonth={draft?.from ?? saved?.from}
            selected={draft}
            onSelect={(range) => {
              setDraft(range);
              if (range?.from && range.to) {
                onChange(dayText(range.from), dayText(range.to));
                setOpen(false);
              }
            }}
          />
          <div className="flex justify-end">
            <Button
              variant="ghost"
              size="sm"
              disabled={saved === undefined && draft === undefined}
              onClick={() => {
                setDraft(undefined);
                onChange("", "");
                setOpen(false);
              }}
            >
              <Trans id="admin.logs.range.clear">Clear dates</Trans>
            </Button>
          </div>
        </div>
      </PopoverPopup>
    </Popover>
  );
}
