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
  value,
  onValueChange,
  options,
  disabled = false,
  size = "sm",
}: {
  label: string;
  value: T;
  onValueChange: (value: T) => void;
  options: readonly { value: T; label: ReactNode; itemClassName?: string }[];
  disabled?: boolean;
  size?: "sm" | "default";
}) {
  return (
    <RadioGroupPrimitive
      aria-label={label}
      value={value}
      disabled={disabled}
      className={segmentedControlRootClassName}
      onValueChange={(next) => onValueChange(next as T)}
    >
      {options.map((option) => (
        <RadioPrimitive.Root
          key={option.value}
          value={option.value}
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
