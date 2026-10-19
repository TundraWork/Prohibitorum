import { Trans } from "@lingui/react/macro";
import { useEffect, useState } from "react";
import { Button } from "@/components/custom/Button";
import { useRegisteredSections } from "@/components/custom/SectionRegistry";

/** Bottom padding that keeps the last row clear of the floating bar. */
export const unsavedReserveClass = "data-reserving:pb-24";

/**
 * The attribute that turns `unsavedReserveClass` on while any section on the
 * page has unsaved edits. Spread it on the element that holds the sections.
 */
export function useUnsavedReserve(): { "data-reserving"?: "true" } {
  const dirty = useRegisteredSections().some((section) => section.dirty);
  return dirty ? { "data-reserving": "true" } : {};
}

/**
 * A floating reminder that a section has unsaved edits the reader has scrolled
 * away from. It shows for the first dirty section that is off screen and
 * offers a jump back to its heading.
 *
 * Where one block holds the Save for edits made in several others, `saveIn`
 * names it: the bar then shows while that block is out of view and leads to it.
 */
export function UnsavedBar({
  saveIn,
}: {
  /** The id of the section that holds Save. */
  saveIn?: string;
}): React.ReactElement | null {
  const dirty = useRegisteredSections().filter((section) => section.dirty);
  const watched = saveIn === undefined ? dirty.map((s) => s.id) : [saveIn];
  const ids = dirty.length === 0 ? "" : watched.join("\n");
  // An element not yet reported counts as visible, so the bar never flashes.
  const [visible, setVisible] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (ids === "" || typeof IntersectionObserver === "undefined") return;
    // The sticky console header covers the top of the window, so a section
    // under it is not on screen.
    const observer = new IntersectionObserver(
      (entries) => {
        setVisible((prev) => {
          const next = { ...prev };
          for (const entry of entries) {
            next[entry.target.id] = entry.isIntersecting;
          }
          return next;
        });
      },
      { rootMargin: "-4rem 0px 0px 0px" },
    );
    for (const id of ids.split("\n")) {
      const element = document.getElementById(id);
      if (element !== null) observer.observe(element);
    }
    return () => observer.disconnect();
  }, [ids]);

  const awayId =
    dirty.length === 0
      ? undefined
      : watched.find((id) => visible[id] === false);
  if (awayId === undefined) return null;

  const title = dirty.find((section) => section.id === awayId)?.title;
  const jump = () => {
    const section = document.getElementById(awayId);
    section?.scrollIntoView({ behavior: "smooth", block: "start" });
    section?.querySelector<HTMLElement>("h2")?.focus({ preventScroll: true });
  };

  return (
    <div
      role="status"
      className="fixed bottom-6 left-1/2 z-20 flex w-max max-w-[calc(100vw-2rem)] -translate-x-1/2 items-center gap-2 rounded-xl bg-popover p-1.5 ps-3.5 text-popover-foreground text-sm shadow-lg/10 ring-1 ring-border"
    >
      <span aria-hidden="true" className="size-1.5 rounded-full bg-warning" />
      <span className="min-w-0 text-balance">
        {saveIn === undefined ? (
          <Trans id="unsaved-bar.message">Unsaved changes in {title}</Trans>
        ) : (
          <Trans id="unsaved-bar.message_save">You have unsaved changes.</Trans>
        )}
      </span>
      <Button onClick={jump} variant="ghost" size="sm" className="shrink-0">
        {saveIn === undefined ? (
          <Trans id="unsaved-bar.jump">Jump to it</Trans>
        ) : (
          <Trans id="unsaved-bar.jump_save">Jump to Save</Trans>
        )}
      </Button>
    </div>
  );
}
