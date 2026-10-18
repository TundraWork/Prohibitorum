import type { ReactNode } from "react";
import { useSectionPanel } from "@/components/custom/Section";
import {
  Frame,
  FrameHeader,
  FramePanel,
  FrameTitle,
} from "@/components/ui/frame";
import { cn } from "@/lib/utils";

/**
 * The console's standard card, capped at the console's content measure.
 *
 * Untitled under a `Section` it is the section's panel; titled it draws its own
 * frame and header. `wide` lets columns use the whole panel.
 */
export function ConsoleCard({
  title,
  children,
  wide = false,
  contentClassName,
}: {
  /** Omitted under a `Section`, which supplies the heading. */
  title?: ReactNode;
  /** Let the content use the panel's full width instead of the reading column. */
  wide?: boolean;
  /** Layout classes for the content column, such as its gap. */
  contentClassName?: string;
  children: ReactNode;
}) {
  const inSection = useSectionPanel(title === undefined);
  const panel = (
    <FramePanel>
      <div className={cn(wide ? "max-w-none" : "max-w-lg", contentClassName)}>
        {children}
      </div>
    </FramePanel>
  );

  if (title === undefined && inSection) return panel;
  if (title === undefined) return <Frame>{panel}</Frame>;

  return (
    <Frame>
      <FrameHeader>
        <FrameTitle>
          <h2>{title}</h2>
        </FrameTitle>
      </FrameHeader>
      {panel}
    </Frame>
  );
}
