// Applies the colour scheme and light/dark mode the app last applied, before
// the first paint, so a page load never flashes another one; the app takes over
// once it runs (AppEnvironment). With nothing stored yet the mode follows the
// device. A classic script, loaded from <head>, because the CSP allows no
// inline script. Keep the key and the shape in step with src/lib/appearance.ts.
(() => {
  const root = document.documentElement;
  let applied = null;
  try {
    applied = JSON.parse(localStorage.getItem("prohibitorum.appearance"));
  } catch {
    // Storage is unavailable or holds something else; use the defaults.
  }
  const scheme = applied?.scheme;
  if (typeof scheme === "string" && /^[a-z]+$/.test(scheme)) {
    root.dataset.scheme = scheme;
  }
  const mode = applied?.mode;
  const dark =
    mode === "dark" ||
    (mode !== "light" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  root.dataset.theme = dark ? "dark" : "light";
  root.classList.toggle("dark", dark);
})();
