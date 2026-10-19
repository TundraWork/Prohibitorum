import type { ReactNode } from "react";
import {
  RadioGroupPrimitive,
  RadioPrimitive,
} from "@/components/ui/radio-group";
import {
  segmentedControlItemVariants,
  segmentedControlRootClassName,
} from "@/lib/segmented-control";
import { cn } from "@/lib/utils";

/**
 * A short, closed choice drawn as one segmented strip, such as `is` / `is not`.
 *
 * It is a radio group, so one segment is always chosen and arrow keys move
 * between them. `itemClassName` styles a segment while it is chosen, for the
 * choice that should read differently from the others.
 */
export function SegmentedControl<T extends string>({
  label,
  labelledBy,
  value,
  onValueChange,
  options,
  disabled = false,
  size = "sm",
}: {
  label: string;
  /** The id of a visible label, which then names the group instead of `label`. */
  labelledBy?: string;
  value: T;
  onValueChange: (value: T) => void;
  options: readonly {
    value: T;
    label: ReactNode;
    itemClassName?: string;
    /** Holds this one segment; give `describedBy` the id of the reason. */
    disabled?: boolean;
    describedBy?: string;
  }[];
  disabled?: boolean;
  size?: "sm" | "default";
}) {
  return (
    <RadioGroupPrimitive
      aria-label={labelledBy === undefined ? label : undefined}
      aria-labelledby={labelledBy}
      value={value}
      disabled={disabled}
      className={segmentedControlRootClassName}
      onValueChange={(next) => onValueChange(next as T)}
    >
      {options.map((option) => (
        <RadioPrimitive.Root
          key={option.value}
          value={option.value}
          disabled={option.disabled}
          aria-describedby={option.describedBy}
          className={cn(
            segmentedControlItemVariants({ size, state: "checked" }),
            "grow",
            option.itemClassName,
          )}
        >
          {option.label}
        </RadioPrimitive.Root>
      ))}
    </RadioGroupPrimitive>
  );
}
