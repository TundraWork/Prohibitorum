import { useAtomValue } from "jotai";
import { type ReactNode, useLayoutEffect } from "react";
import { publicThemeAtom, themeAtom } from "@/components/custom/ThemeSelect";

/**
 * What every screen needs whether or not a route drew: the theme. It sits
 * outside the router, so a root route that fails still draws its error in it.
 *
 * The theme is the one the public pages force, if any, or else the visitor's.
 * It is written on `<html>` before the browser paints, so moving between the
 * console and a public page never shows a frame in the other theme, and popups
 * rendered under `body` follow it too.
 */
export function AppEnvironment({ children }: { children: ReactNode }) {
  const chosen = useAtomValue(themeAtom);
  const forced = useAtomValue(publicThemeAtom);
  const theme = forced ?? chosen;
  useLayoutEffect(() => {
    // coss styles dark mode off the `dark` class; `data-theme` stays for the
    // rest of the app and for popups rendered under `body`.
    const write = (resolved: "light" | "dark") => {
      const root = document.documentElement;
      root.dataset.theme = resolved;
      root.classList.toggle("dark", resolved === "dark");
    };
    if (theme !== "system") {
      write(theme);
      return;
    }
    const preference = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => write(preference.matches ? "dark" : "light");
    apply();
    preference.addEventListener("change", apply);
    return () => preference.removeEventListener("change", apply);
  }, [theme]);
  return children;
}
