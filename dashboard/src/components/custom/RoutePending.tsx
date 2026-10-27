import { useLingui } from "@lingui/react/macro";
import { useRouterState } from "@tanstack/react-router";
import { type ReactNode, use } from "react";
import { PageBody } from "@/components/custom/PageBody";
import { PublicStage } from "@/components/custom/PublicStage";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";
import { Frame, FramePanel } from "@/components/ui/frame";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { PadPending } from "@/pages/launcher/Launcher";

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
      return "public";
    },
  });
  if (where === "public") return <Loading>{<StepSkeleton />}</Loading>;
  if (where === "console") return <Loading>{<ConsolePageSkeleton />}</Loading>;
  return (
    <Loading>
      {shell === "console" ? (
        <ConsoleShellSkeleton />
      ) : shell === "launcher" ? (
        <LauncherShellSkeleton />
      ) : (
        <PublicShellSkeleton />
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
function ConsolePageSkeleton() {
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
function ToolbarSkeleton() {
  return (
    <div className="flex h-auto items-center justify-between gap-3 px-(--public-edge) pt-(--public-edge)">
      <Skeleton className="h-9 w-44 rounded-[14px]" />
      <Skeleton className="h-9 w-24 rounded-[14px]" />
    </div>
  );
}

function PublicShellSkeleton() {
  return (
    <div className="min-h-(--app-viewport-height) bg-background">
      <ToolbarSkeleton />
      <PublicStage frame="plain">
        <StepSkeleton />
      </PublicStage>
    </div>
  );
}

function LauncherShellSkeleton() {
  return (
    <div className="min-h-(--app-viewport-height) bg-background">
      <ToolbarSkeleton />
      <div className="mx-auto flex w-full max-w-5xl flex-col gap-8 px-5 py-10">
        <div className="flex flex-col gap-2.5">
          <Skeleton className="h-9 w-56" />
          <Skeleton className="h-4 w-72 max-w-full" />
        </div>
        <PadPending />
      </div>
    </div>
  );
}

function ConsoleShellSkeleton() {
  return (
    <SidebarProvider>
      <Sidebar variant="inset" mobileDescription={null}>
        <SidebarHeader>
          <Skeleton className="m-2 h-9 w-40" />
        </SidebarHeader>
        <SidebarContent>
          {["a", "b"].map((group) => (
            <SidebarGroup key={group}>
              <SidebarMenu>
                {["1", "2", "3", "4"].map((item) => (
                  <SidebarMenuItem key={item}>
                    <SidebarMenuSkeleton showIcon />
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroup>
          ))}
        </SidebarContent>
      </Sidebar>
      <SidebarInset className="min-w-0">
        <div className="flex h-14 shrink-0 items-center gap-2 rounded-t-xl border-b px-4">
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="mx-auto w-full max-w-6xl px-6 py-8 lg:px-10">
          <ConsolePageSkeleton />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
