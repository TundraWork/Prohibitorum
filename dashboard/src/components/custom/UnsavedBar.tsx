import { Trans } from "@lingui/react/macro";
import { useEffect, useState } from "react";
import { Button } from "@/components/custom/Button";
import { useRegisteredSections } from "@/components/custom/SectionRegistry";

/**
 * A floating reminder that a section has unsaved edits the reader has scrolled
 * away from. It shows for the first dirty section that is off screen and
 * offers a jump back to its heading.
 */
export function UnsavedBar(): React.ReactElement | null {
  const dirty = useRegisteredSections().filter((section) => section.dirty);
  const ids = dirty.map((section) => section.id).join("\n");
  // An element not yet reported counts as visible, so the bar never flashes.
  const [visible, setVisible] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (ids === "" || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver((entries) => {
      setVisible((prev) => {
        const next = { ...prev };
        for (const entry of entries) {
          next[entry.target.id] = entry.isIntersecting;
        }
        return next;
      });
    });
    for (const id of ids.split("\n")) {
      const element = document.getElementById(id);
      if (element !== null) observer.observe(element);
    }
    return () => observer.disconnect();
  }, [ids]);

  const away = dirty.find((section) => visible[section.id] === false);
  if (away === undefined) return null;

  const { title } = away;
  const jump = () => {
    const section = document.getElementById(away.id);
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
        <Trans id="unsaved-bar.message">Unsaved changes in {title}</Trans>
      </span>
      <Button onClick={jump} variant="ghost" size="sm" className="shrink-0">
        <Trans id="unsaved-bar.jump">Jump to it</Trans>
      </Button>
    </div>
  );
}
