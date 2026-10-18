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
 * Under a `Section` it is the section's panel and takes no title: the section
 * names the block. A titled card stands alone, as a frame with its own header,
 * for a dialog's content or a page block that has no section.
 *
 * The measure is a choice: the default suits a form read line by line, and
 * `wide` gives content laid out in columns the whole panel.
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
  const inSection = useSectionPanel();
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
