import type { LoginGradient } from "@/api/raw-paths";

/** The presets in the order the settings offer them; the server accepts the same IDs. */
export const loginGradients: LoginGradient[] = [
  "dawn",
  "lagoon",
  "aurora",
  "dusk",
  "mist",
  "ember",
];

/** Fine grain over every gradient, so wide soft ramps do not band. */
const grain =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='180' height='180'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='.85' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 .07 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")";

/**
 * Each preset as a CSS `background`. They do not follow light and dark mode:
 * what the administrator picks is what the sign-in page looks like. `lagoon`,
 * the default, takes its colours from near the theme's hue.
 */
export const gradientBackground: Record<LoginGradient, string> = {
  dawn: [
    grain,
    "radial-gradient(120% 90% at 12% 8%, oklch(97% 0.045 85) 0%, transparent 55%)",
    "radial-gradient(90% 80% at 92% 95%, oklch(76% 0.1 18) 0%, transparent 62%)",
    "linear-gradient(160deg, oklch(92% 0.05 62), oklch(83% 0.075 32))",
  ].join(", "),
  lagoon: [
    grain,
    "radial-gradient(100% 80% at 8% 0%, oklch(87% 0.07 185) 0%, transparent 60%)",
    "radial-gradient(85% 75% at 100% 100%, oklch(40% 0.1 258) 0%, transparent 70%)",
    "linear-gradient(155deg, oklch(74% 0.08 200), oklch(50% 0.1 238))",
  ].join(", "),
  aurora: [
    grain,
    "radial-gradient(60% 50% at 18% 28%, oklch(72% 0.15 160 / 0.75), transparent 70%)",
    "radial-gradient(55% 45% at 76% 18%, oklch(66% 0.12 205 / 0.65), transparent 70%)",
    "radial-gradient(70% 60% at 82% 92%, oklch(52% 0.16 300 / 0.6), transparent 70%)",
    "linear-gradient(180deg, oklch(24% 0.04 250), oklch(17% 0.03 272))",
  ].join(", "),
  dusk: [
    grain,
    "radial-gradient(95% 60% at 50% 112%, oklch(83% 0.11 62) 0%, transparent 62%)",
    "linear-gradient(180deg, oklch(40% 0.1 275), oklch(58% 0.13 328) 68%, oklch(72% 0.12 22))",
  ].join(", "),
  mist: [
    grain,
    "radial-gradient(90% 70% at 18% 8%, oklch(99% 0.005 220) 0%, transparent 60%)",
    "radial-gradient(80% 70% at 92% 100%, oklch(81% 0.028 232) 0%, transparent 65%)",
    "linear-gradient(160deg, oklch(95% 0.008 210), oklch(87% 0.016 226))",
  ].join(", "),
  ember: [
    grain,
    "radial-gradient(70% 60% at 8% 100%, oklch(63% 0.17 45 / 0.78), transparent 70%)",
    "radial-gradient(60% 50% at 92% 8%, oklch(47% 0.15 18 / 0.62), transparent 70%)",
    "linear-gradient(170deg, oklch(26% 0.03 40), oklch(17% 0.02 22))",
  ].join(", "),
};
