import type { ReactNode } from "react";
import { OutlineRail } from "@/components/custom/OutlineRail";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { UnsavedBar } from "@/components/custom/UnsavedBar";

/**
 * The frame of an application's page: the object header, the sections in one
 * column and, from `lg`, an outline rail beside them.
 *
 * Every `Section` among the children registers itself here, which is what the
 * rail's links and the unsaved-changes bar are drawn from. Below `lg` the rail
 * is left out, its `facts` move under the header, and the sections keep the
 * whole width.
 */
export function AppDetailLayout({
  header,
  facts,
  children,
}: {
  /** An `ObjectHeader`. */
  header: ReactNode;
  /** Shown under the outline, such as an issuer or an entity ID. */
  facts?: ReactNode;
  /** The sections in order, the danger section last. */
  children: ReactNode;
}): React.ReactElement {
  return (
    <SectionRegistryProvider>
      {header}
      {facts !== undefined && (
        <div className="mt-4 text-muted-foreground text-sm lg:hidden">
          {facts}
        </div>
      )}
      <div className="mt-8 grid items-start gap-10 lg:grid-cols-[minmax(0,1fr)_200px]">
        <div className="flex min-w-0 flex-col gap-6">{children}</div>
        <OutlineRail facts={facts} className="hidden lg:grid" />
      </div>
      <UnsavedBar />
    </SectionRegistryProvider>
  );
}
