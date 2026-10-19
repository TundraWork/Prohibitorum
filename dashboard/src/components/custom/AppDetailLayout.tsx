import type { ReactNode } from "react";
import { OutlineRail } from "@/components/custom/OutlineRail";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import {
  UnsavedBar,
  unsavedReserveClass,
  useUnsavedReserve,
} from "@/components/custom/UnsavedBar";

/**
 * The frame of an application's page: the object header, the sections in one
 * column and, from `lg`, an outline rail beside them.
 *
 * Every `Section` among the children registers itself here, which is what the
 * rail's links and the unsaved-changes bar are drawn from. Below `lg` the rail
 * is left out, its `facts` move under the header, and the sections keep the
 * whole width.
 *
 * While any section has unsaved edits the columns reserve room at the bottom
 * for the floating unsaved bar, so scrolled to the end the last row is never
 * under it.
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
      <Columns facts={facts}>{children}</Columns>
      <UnsavedBar />
    </SectionRegistryProvider>
  );
}

function Columns({
  facts,
  children,
}: {
  facts?: ReactNode;
  children: ReactNode;
}): React.ReactElement {
  const reserve = useUnsavedReserve();
  return (
    <div
      data-slot="detail-columns"
      {...reserve}
      className={`mt-8 grid items-start gap-10 ${unsavedReserveClass} lg:grid-cols-[minmax(0,1fr)_200px]`}
    >
      <div className="flex min-w-0 flex-col gap-6">{children}</div>
      <OutlineRail facts={facts} className="hidden lg:grid" />
    </div>
  );
}
