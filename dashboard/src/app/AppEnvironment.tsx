import { useQuery } from "@tanstack/react-query";
import { useAtomValue } from "jotai";
import { type ReactNode, useLayoutEffect } from "react";
import { publicConfigQueryOptions } from "@/api/queries";
import {
  useAccountAppearanceSync,
  useChosenAppearance,
} from "@/app/use-appearance";
import {
  appliedAppearanceKey,
  forcedModeAtom,
  resolveAppearance,
} from "@/lib/appearance";

/**
 * What every screen needs whether or not a route drew: the colour scheme and
 * the light or dark mode. It sits outside the router, so a root route that
 * fails still draws its error in it.
 *
 * Both are written on `<html>` before the browser paints, so moving between the
 * console and a public page never shows a frame in the other mode, and popups
 * rendered under `body` follow them too. What was applied is kept for
 * `/appearance-guard.js`, which applies it again before the next page load
 * paints. Until the instance's default scheme is known, a browser without a
 * choice keeps the scheme that script applied.
 */
export function AppEnvironment({ children }: { children: ReactNode }) {
  useAccountAppearanceSync();
  const { scheme, mode } = useChosenAppearance();
  const forced = useAtomValue(forcedModeAtom);
  const { data: config } = useQuery({
    ...publicConfigQueryOptions(),
    enabled: false,
  });
  const known = scheme !== null || config !== undefined;
  const applied = resolveAppearance(
    { scheme, mode },
    config?.loginAppearance.scheme,
    forced,
  );
  useLayoutEffect(() => {
    if (!known) return;
    document.documentElement.dataset.scheme = applied.scheme;
  }, [known, applied.scheme]);
  useLayoutEffect(() => {
    if (!known) return;
    try {
      localStorage.setItem(
        appliedAppearanceKey,
        JSON.stringify({ scheme: applied.scheme, mode: applied.mode }),
      );
    } catch {
      // Without storage the next load starts from the defaults.
    }
  }, [known, applied.scheme, applied.mode]);
  useLayoutEffect(() => {
    // coss styles dark mode off the `dark` class; `data-theme` stays for the
    // rest of the app and for popups rendered under `body`.
    const write = (resolved: "light" | "dark") => {
      const root = document.documentElement;
      root.dataset.theme = resolved;
      root.classList.toggle("dark", resolved === "dark");
    };
    if (applied.mode !== "system") {
      write(applied.mode);
      return;
    }
    const preference = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => write(preference.matches ? "dark" : "light");
    apply();
    preference.addEventListener("change", apply);
    return () => preference.removeEventListener("change", apply);
  }, [applied.mode]);
  return children;
}
