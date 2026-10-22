import { cva } from "class-variance-authority";
import { createContext, type ReactNode, use, useMemo, useState } from "react";
import type { LoginCardPosition } from "@/api/raw-paths";
import { publicCardPlacement } from "@/components/custom/login-appearance/card-position";
import type { SurfaceStyle } from "@/components/custom/login-appearance/surface-style";
import { cn } from "@/lib/utils";

type StageWidth = "default" | "wide";

type StageSlots = {
  strip: HTMLElement | null;
  footer: HTMLElement | null;
  setWidth: (width: StageWidth) => void;
  /** False outside a stage, where a step draws its strip and footer in place. */
  inStage: boolean;
};

const StageSlotsContext = createContext<StageSlots | null>(null);

const noStage: StageSlots = {
  strip: null,
  footer: null,
  setWidth: () => undefined,
  inStage: false,
};

/**
 * Where a step puts what belongs around its card: the strip above it, the
 * footer under it and a request for the wide width. Outside a stage there is
 * nowhere to put them, which `inStage` says.
 */
export function useStageSlots(): StageSlots {
  return use(StageSlotsContext) ?? noStage;
}

const placement =
  "flex min-h-[calc(var(--app-viewport-height)-var(--public-edge)-2.25rem)] w-full items-start justify-center px-(--public-edge) sm:items-center py-4";

const frameStyle = cva(
  "relative flex w-full flex-col rounded-[28px] p-1.5 max-sm:max-w-none [--stage-inset:--spacing(5)] sm:[--stage-inset:--spacing(7)]",
  {
    variants: {
      frame: {
        frosted:
          "bg-background/60 shadow-2xl shadow-black/30 ring-1 ring-white/40 backdrop-blur-2xl",
        plain: "bg-muted/72 shadow-xs/5 ring-1 ring-border dark:bg-black/30",
      },
      width: {
        default: "sm:w-[440px]",
        wide: "sm:w-[480px]",
      },
    },
  },
);

/**
 * The stage every public page sits on: a frame whose arcs are concentric with
 * the card inside it, a strip above the card and a footer under it for what a
 * step wants beside its body. It sits under the toolbar on a narrow window,
 * is centred in the window from `sm` and grows with its content, so a tall
 * page scrolls with the window. Frosted
 * over a picture, gradient or colour; plain when the page's own background
 * shows through.
 */
export function PublicStage({
  width = "default",
  position = "center",
  frame,
  card,
  after,
  children,
}: {
  width?: StageWidth;
  position?: LoginCardPosition;
  frame: "frosted" | "plain";
  card?: SurfaceStyle;
  /** Shown in the centred column under the frame. */
  after?: ReactNode;
  children: ReactNode;
}) {
  const [strip, setStrip] = useState<HTMLElement | null>(null);
  const [footer, setFooter] = useState<HTMLElement | null>(null);
  const [wanted, setWanted] = useState<StageWidth>("default");
  const slots = useMemo<StageSlots>(
    () => ({ strip, footer, setWidth: setWanted, inStage: true }),
    [strip, footer],
  );
  const shown = wanted === "wide" ? "wide" : width;

  return (
    <main
      data-slot="stage"
      data-width={shown}
      data-frame={frame}
      data-position={position}
      className={cn(
        placement,
        "text-pretty",
        publicCardPlacement({ position }),
      )}
    >
      <div
        data-slot="stage-column"
        className="flex w-full flex-col items-center gap-3 sm:w-auto"
      >
        <section
          data-slot="stage-frame"
          className={frameStyle({ frame, width: shown })}
        >
          <div
            ref={setStrip}
            data-slot="stage-strip"
            // The strip and footer start on the card's content edge: its inset plus
            // its 1px border.
            className="flex items-center gap-3 px-[calc(var(--stage-inset)+1px)] pt-1.5 pb-3 empty:hidden"
          />
          <div
            data-slot="stage-card"
            style={card?.style}
            className={cn(
              "relative flex min-w-0 flex-col rounded-[22px] border not-dark:bg-clip-padding p-(--stage-inset) text-card-foreground shadow-xs/5 before:pointer-events-none before:absolute before:inset-0 before:rounded-[21px] before:shadow-[0_1px_--theme(--color-black/4%)] dark:before:shadow-[0_-1px_--theme(--color-white/6%)]",
              card?.className ?? "bg-card",
            )}
          >
            <StageSlotsContext value={slots}>{children}</StageSlotsContext>
          </div>
          <div
            ref={setFooter}
            data-slot="stage-footer"
            className="flex items-center gap-3 px-[calc(var(--stage-inset)+1px)] pt-3 pb-1.5 text-base empty:hidden"
          />
        </section>
        {after}
      </div>
    </main>
  );
}
