import { Alert } from "@heroui/react";
import { cva } from "class-variance-authority";
import type { ComponentProps } from "react";

/**
 * Alert for the surface plane. Inside a Card the library's surface shadow
 * reads as a raised block, so this variant drops it and fills the alert with
 * the status's soft tint instead. Slots, statuses and sub-components stay
 * HeroUI's.
 */
const surfaceAlert = cva("shadow-none", {
  variants: {
    status: {
      default: "bg-surface-secondary",
      accent: "bg-accent-soft",
      success: "bg-success-soft",
      warning: "bg-warning-soft",
      danger: "bg-danger-soft",
    },
  },
  defaultVariants: { status: "default" },
});

function SurfaceAlertRoot({
  status = "default",
  className,
  ...props
}: ComponentProps<typeof Alert>) {
  return (
    <Alert
      status={status}
      className={surfaceAlert({ status, className })}
      {...props}
    />
  );
}

export const SurfaceAlert = Object.assign(SurfaceAlertRoot, {
  Indicator: Alert.Indicator,
  Content: Alert.Content,
  Title: Alert.Title,
  Description: Alert.Description,
});
