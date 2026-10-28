/*
 * The launcher's and the console's whole-window placeholders (`RoutePending`).
 * They load with the console's code, not with every page, since only a
 * console or launcher address draws them.
 */
import {
  ConsolePageSkeleton,
  ToolbarSkeleton,
} from "@/components/custom/RoutePending";
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

export function LauncherShellSkeleton() {
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

export function ConsoleShellSkeleton() {
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
