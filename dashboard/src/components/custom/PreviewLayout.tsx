import { Alert, Skeleton } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { cva, type VariantProps } from "class-variance-authority";
import type { ReactNode } from "react";

export function PageHeader({
  eyebrow,
  title,
  description,
}: {
  eyebrow: ReactNode;
  title: ReactNode;
  description: ReactNode;
}) {
  return (
    <header className="flex flex-col gap-2">
      <p className="text-sm text-muted">{eyebrow}</p>
      <h1 className="text-2xl font-semibold">{title}</h1>
      <p className="text-muted">{description}</p>
    </header>
  );
}

export function RewriteNotice() {
  return (
    <Alert status="warning">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title>
          <Trans id="preview.notice">
            The frontend is being rebuilt. Sign-in and other features are
            temporarily unavailable.
          </Trans>
        </Alert.Title>
      </Alert.Content>
    </Alert>
  );
}

const skeletonLine = cva("h-4", {
  variants: {
    width: {
      full: "w-full",
      medium: "w-3/4",
      short: "w-1/2",
    },
  },
  defaultVariants: { width: "full" },
});

function SkeletonLine({ width }: VariantProps<typeof skeletonLine>) {
  return <Skeleton className={skeletonLine({ width })} />;
}

export function PageSkeleton() {
  return (
    <div aria-hidden="true" className="flex flex-col gap-3">
      <SkeletonLine />
      <SkeletonLine width="medium" />
      <SkeletonLine width="short" />
    </div>
  );
}
