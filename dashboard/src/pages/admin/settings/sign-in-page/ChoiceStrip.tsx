import { type ReactNode, useId } from "react";
import { SegmentedControl } from "@/components/custom/SegmentedControl";

/**
 * A short, closed choice with its name above it, drawn as a segmented strip
 * across the column. The visible name is what names the radio group.
 */
export function ChoiceStrip<T extends string>({
  label,
  value,
  options,
  disabled = false,
  onChange,
}: {
  label: string;
  value: T;
  options: readonly { value: T; label: ReactNode }[];
  disabled?: boolean;
  onChange: (value: T) => void;
}) {
  const labelId = useId();
  return (
    <div className="flex flex-col gap-2">
      <span id={labelId} className="font-medium text-sm">
        {label}
      </span>
      <SegmentedControl
        label={label}
        labelledBy={labelId}
        value={value}
        size="default"
        disabled={disabled}
        className="w-full"
        // A long name wraps inside its segment rather than pushing the strip
        // past its column.
        options={options.map((option) => ({
          ...option,
          itemClassName:
            "h-auto min-h-7.5 min-w-0 shrink whitespace-normal px-1.5 py-1 text-center leading-tight sm:min-h-6.5",
        }))}
        onValueChange={onChange}
      />
    </div>
  );
}
