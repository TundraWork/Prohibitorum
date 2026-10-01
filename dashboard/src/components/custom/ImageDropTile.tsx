import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { DropZone, FileTrigger } from "react-aria-components";
import { Button } from "@/components/custom/Button";

/**
 * A dashed tile that takes image files, either dropped on it or chosen through
 * the button that fills it. It sits at the end of a grid of images, the same
 * size as the tiles before it, and turns to the accent while a file is dragged
 * over it.
 *
 * The tile only hands the files over. Checking them, the request and the
 * outcome are the caller's, which also decides the tile's size through
 * `className`. The sign-in page's images and the profile page's avatar gallery
 * both end with one.
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
  return (
    <DropZone
      aria-label={ariaLabel}
      isDisabled={busy}
      onDrop={async (event) => {
        const items = event.items.filter((item) => item.kind === "file");
        const files = await Promise.all(
          (allowsMultiple ? items : items.slice(0, 1)).map((item) =>
            item.getFile(),
          ),
        );
        if (files.length > 0) onFiles(files);
      }}
      className={`group flex rounded-[0.375rem] border border-dashed border-border text-sm text-muted outline-none transition-colors duration-150 data-[drop-target]:border-accent data-[drop-target]:bg-accent-soft data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${className ?? ""}`}
    >
      <FileTrigger
        allowsMultiple={allowsMultiple}
        acceptedFileTypes={acceptedFileTypes}
        onSelect={(list) => {
          if (list && list.length > 0) onFiles(Array.from(list));
        }}
      >
        <Button
          variant="ghost"
          size="sm"
          isDisabled={isDisabled}
          isPending={isPending}
          className="h-full w-full flex-col gap-1.5 text-muted"
        >
          {({ isPending: pending }) => (
            <>
              {!pending && (
                <Icon size={20} strokeWidth={1.75} aria-hidden="true" />
              )}
              <span>{label}</span>
            </>
          )}
        </Button>
      </FileTrigger>
    </DropZone>
  );
}
