import { useEffect, useRef } from "react";

/**
 * Runs `onRestore` when the browser shows this page again from the
 * back-forward cache, where state kept across a full-page navigation (a
 * pending button) would otherwise stay stuck.
 */
export function usePageRestore(onRestore: () => void) {
  const latest = useRef(onRestore);
  latest.current = onRestore;
  useEffect(() => {
    const restore = (event: PageTransitionEvent) => {
      if (event.persisted) latest.current();
    };
    window.addEventListener("pageshow", restore);
    return () => window.removeEventListener("pageshow", restore);
  }, []);
}
