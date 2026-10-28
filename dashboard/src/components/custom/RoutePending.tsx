import { useLingui } from "@lingui/react/macro";
import { useRouterState } from "@tanstack/react-router";
import { lazy, type ReactNode, Suspense, use, useEffect } from "react";
import { createPortal } from "react-dom";
import { PageBody } from "@/components/custom/PageBody";
import { PublicStage, useStageSlots } from "@/components/custom/PublicStage";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";
import { Frame, FramePanel } from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";

// The console's and the launcher's shells come with their own code; until it
// arrives, the window stays as it is.
const ConsoleShellSkeleton = lazy(() =>
  import("@/components/custom/ShellSkeletons").then((module) => ({
    default: module.ConsoleShellSkeleton,
  })),
);
const LauncherShellSkeleton = lazy(() =>
  import("@/components/custom/ShellSkeletons").then((module) => ({
    default: module.LauncherShellSkeleton,
  })),
);

/**
 * What a page shows while its route is still loading: the shape of the page
 * it is about to become, drawn in placeholder bars, so the wait reads as the
 * page arriving rather than as an empty window, and the content lands in the
 * place the bars held.
 *
 * Inside a layout the layout is already drawn, so only the page's own part is
 * a placeholder: a step on the public stage, a page header and its sections in
 * the console. Before any layout has drawn — the first load, when the router
 * holds the whole window until every route on the way has loaded — the
 * placeholder is the destination's whole shell, picked from the routes being
 * loaded: the console's sidebar and panel, the launcher's toolbar and pad, or
 * the public toolbar and stage.
 *
 * The bars are decorative and hidden from assistive technology; one hidden
 * status says the page is loading.
 */
export function RoutePending() {
  const where = use(RouteLayoutContext);
  const shell = useRouterState({
    select: (state) => {
      const ids = state.matches.map((match) => match.routeId as string);
      if (ids.some((id) => id.startsWith("/_protected"))) return "console";
      if (ids.some((id) => id.startsWith("/_launcher"))) return "launcher";
      if (ids.some((id) => consentRoutes.includes(id))) return "consent";
      return "public";
    },
  });
  const step = shell === "consent" ? <ConsentSkeleton /> : <StepSkeleton />;
  if (where === "public") return <Loading>{step}</Loading>;
  if (where === "console") return <Loading>{<ConsolePageSkeleton />}</Loading>;
  return (
    <Loading>
      {shell === "console" ? (
        <Suspense fallback={null}>
          <ConsoleShellSkeleton />
        </Suspense>
      ) : shell === "launcher" ? (
        <Suspense fallback={null}>
          <LauncherShellSkeleton />
        </Suspense>
      ) : (
        <PublicShellSkeleton>{step}</PublicShellSkeleton>
      )}
    </Loading>
  );
}

function Loading({ children }: { children: ReactNode }) {
  const { t } = useLingui();
  return (
    <>
      <span role="status" className="sr-only">
        {t({ id: "route.loading", message: "Loading" })}
      </span>
      <div aria-hidden="true" className="contents">
        {children}
      </div>
    </>
  );
}

/** A public step's anatomy: its heading, a line of description, two fields and its action. */
function StepSkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2.5">
        <Skeleton className="h-7 w-3/5" />
        <Skeleton className="h-4 w-4/5" />
      </div>
      <div className="flex flex-col gap-4">
        {["a", "b"].map((field) => (
          <div key={field} className="flex flex-col gap-2">
            <Skeleton className="h-3.5 w-1/4" />
            <Skeleton className="h-9 w-full" />
          </div>
        ))}
      </div>
      <Skeleton className="h-10 w-full" />
    </div>
  );
}

/** A console page: its header, then two sections. */
export function ConsolePageSkeleton() {
  return (
    <PageBody>
      <header className="flex flex-col gap-2.5">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </header>
      {["a", "b"].map((section) => (
        <Frame key={section}>
          <FramePanel>
            <div className="flex flex-col gap-3">
              <Skeleton className="h-4 w-2/5" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-4/5" />
            </div>
          </FramePanel>
        </Frame>
      ))}
    </PageBody>
  );
}

/** The toolbar's two capsules, before the instance's name and icon are known. */
export function ToolbarSkeleton() {
  return (
    <div className="flex h-auto items-center justify-between gap-3 px-(--public-edge) pt-(--public-edge)">
      <Skeleton className="h-9 w-44 rounded-[14px]" />
      <Skeleton className="h-9 w-24 rounded-[14px]" />
    </div>
  );
}

function PublicShellSkeleton({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-(--app-viewport-height) bg-background">
      <ToolbarSkeleton />
      <PublicStage frame="plain">{children}</PublicStage>
    </div>
  );
}

/** The routes that draw an application's consent request (`ConsentRequest`). */
const consentRoutes = ["/_public/consent", "/_public/saml-consent"];

/**
 * A consent request's anatomy, so the request lands in the bars' place: who is
 * asking in the strip above the card, the question, the signed-in account, the
 * list of what is asked for and a footnote, and the two decisions under the
 * card, on the wide card the request asks for.
 */
function ConsentSkeleton() {
  const slots = useStageSlots();
  const { setWidth } = slots;
  useEffect(() => {
    setWidth("wide");
    return () => setWidth("default");
  }, [setWidth]);
  const strip = (
    <div aria-hidden="true" className="flex items-center gap-3.5 py-1.5">
      <Skeleton className="size-13 shrink-0 rounded-[14px]" />
      <div className="flex flex-col gap-2">
        <Skeleton className="h-3.5 w-16" />
        <Skeleton className="h-4 w-28" />
      </div>
    </div>
  );
  const footer = (
    <div aria-hidden="true" className="grid w-full grid-cols-2 gap-2">
      <Skeleton className="h-10" />
      <Skeleton className="h-10" />
    </div>
  );
  return (
    <div className="flex flex-col gap-4">
      {slots.strip ? createPortal(strip, slots.strip) : null}
      <Skeleton className="h-7 w-3/5" />
      <div className="flex items-center gap-2.5 rounded-xl bg-muted px-3 py-2">
        <Skeleton className="size-6 shrink-0 rounded-full" />
        <Skeleton className="h-4 w-40" />
      </div>
      <div className="flex flex-col gap-2">
        <Skeleton className="h-3.5 w-32" />
        <ul className="divide-y overflow-hidden rounded-xl border bg-background shadow-xs/5">
          {["a", "b", "c", "d"].map((row) => (
            <li key={row} className="flex items-start gap-3 px-3.5 py-3">
              <Skeleton className="size-7 shrink-0 rounded-lg" />
              <div className="flex min-w-0 flex-1 flex-col gap-2 pt-0.5">
                <Skeleton className="h-3.5 w-1/3" />
                <Skeleton className="h-3 w-2/3" />
              </div>
            </li>
          ))}
        </ul>
      </div>
      <div className="flex flex-col gap-1.5">
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-1/2" />
      </div>
      {slots.footer ? createPortal(footer, slots.footer) : null}
    </div>
  );
}
