import type { LucideIcon } from "lucide-react";
import { type DragEvent, type ReactNode, useRef, useState } from "react";
import { buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

const carriesFiles = (event: DragEvent) =>
  Array.from(event.dataTransfer?.types ?? []).includes("Files");

/**
 * A dashed tile that takes image files, either dropped on it or chosen through
 * the button that fills it. It sits at the end of a grid of images, the same
 * size as the tiles before it, and turns to the primary tint while a file is
 * dragged over it.
 *
 * The button is a label over a visually hidden file input, so the browser's own
 * picker, keyboard focus and form semantics all apply. The tile only hands the
 * files over. Checking them, the request and the outcome are the caller's, which
 * also decides the tile's size through `className`. The sign-in page's images
 * and the profile page's avatar gallery both end with one.
 */
export function ImageDropTile({
  label,
  ariaLabel,
  icon: Icon,
  acceptedFileTypes,
  allowsMultiple = false,
  isPending = false,
  isDisabled = false,
  onFiles,
  className,
}: {
  /** The button's text. */
  label: ReactNode;
  /** Names the drop target, which says both ways to add a file. */
  ariaLabel: string;
  icon: LucideIcon;
  acceptedFileTypes: readonly string[];
  allowsMultiple?: boolean;
  isPending?: boolean;
  isDisabled?: boolean;
  /** The chosen or dropped files, in order; one at most unless `allowsMultiple`. */
  onFiles: (files: File[]) => void;
  /** Sizing for the tile, such as its aspect ratio. */
  className?: string;
}) {
  const busy = isDisabled || isPending;
  const [over, setOver] = useState(false);
  // dragenter/dragleave also fire for the children; count them so the tint
  // holds until the drag has left the tile and everything inside it.
  const depth = useRef(0);

  function take(files: ArrayLike<File> | null | undefined) {
    const list = Array.from(files ?? []);
    const taken = allowsMultiple ? list : list.slice(0, 1);
    if (taken.length > 0) onFiles(taken);
  }

  return (
    // biome-ignore lint/a11y/useSemanticElements: a drop target is a group, not a fieldset
    <div
      role="group"
      aria-label={ariaLabel}
      data-drop-target={over ? "true" : undefined}
      onDragEnter={(event) => {
        if (busy || !carriesFiles(event)) return;
        event.preventDefault();
        depth.current += 1;
        setOver(true);
      }}
      onDragOver={(event) => {
        if (busy || !carriesFiles(event)) return;
        event.preventDefault();
      }}
      onDragLeave={(event) => {
        if (!carriesFiles(event)) return;
        depth.current = Math.max(0, depth.current - 1);
        if (depth.current === 0) setOver(false);
      }}
      onDrop={(event) => {
        event.preventDefault();
        depth.current = 0;
        setOver(false);
        if (busy) return;
        take(event.dataTransfer?.files);
      }}
      className={cn(
        "flex rounded-lg border border-dashed text-muted-foreground text-sm transition-colors duration-150 data-[drop-target]:border-primary data-[drop-target]:bg-primary/8",
        className,
      )}
    >
      <label
        className={cn(
          buttonVariants({ variant: "ghost", size: "sm" }),
          // `sm:h-auto` is what overrides the size variant's `sm:h-7` through twMerge.
          "h-auto flex-1 sm:h-auto cursor-pointer flex-col justify-center gap-1.5 text-muted-foreground has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring has-[:focus-visible]:ring-offset-1 has-[:focus-visible]:ring-offset-background",
          busy && "pointer-events-none opacity-64",
        )}
      >
        <input
          type="file"
          className="sr-only"
          accept={acceptedFileTypes.join(",")}
          multiple={allowsMultiple}
          disabled={busy}
          onChange={(event) => {
            take(event.currentTarget.files);
            // Choosing the same file again must still fire a change.
            event.currentTarget.value = "";
          }}
        />
        {isPending ? (
          <Spinner aria-hidden="true" />
        ) : (
          <Icon size={20} strokeWidth={1.75} aria-hidden="true" />
        )}
        <span>{label}</span>
      </label>
    </div>
  );
}
