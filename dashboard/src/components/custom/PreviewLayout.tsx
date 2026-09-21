import { Link, Skeleton } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { createLink, Outlet } from "@tanstack/react-router";
import { cva, type VariantProps } from "class-variance-authority";
import type { ReactNode } from "react";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

const NavigationLink = createLink(Link);

export function PreviewLayout() {
  const { t } = useLingui();
  return (
    <div className="flex flex-col gap-6">
      <nav
        className="flex flex-wrap items-center gap-x-6 gap-y-3"
        aria-label={t({
          id: "navigation.preview",
          message: "Preview navigation",
        })}
      >
        <NavigationLink to="/preview/components">
          <Trans id="navigation.components">Components</Trans>
        </NavigationLink>
        <NavigationLink to="/preview/api">
          <Trans id="navigation.api">Public API</Trans>
        </NavigationLink>
        <NavigationLink to="/login" preload={false}>
          <Trans id="navigation.login">Sign in</Trans>
        </NavigationLink>
        <NavigationLink to="/" preload={false}>
          <Trans id="console.home">Console home</Trans>
        </NavigationLink>
        {import.meta.env.DEV && (
          <NavigationLink to="/__dev/forms">
            <Trans id="navigation.forms">Form verification (development)</Trans>
          </NavigationLink>
        )}
      </nav>
      <Outlet />
    </div>
  );
}

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
    <SurfaceAlert status="warning">
      <SurfaceAlert.Indicator />
      <SurfaceAlert.Content>
        <SurfaceAlert.Title>
          <Trans id="preview.notice">
            This is a public component preview. Account management and
            administration pages are not available yet.
          </Trans>
        </SurfaceAlert.Title>
      </SurfaceAlert.Content>
    </SurfaceAlert>
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
