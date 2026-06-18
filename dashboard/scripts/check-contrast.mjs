// scripts/check-contrast.mjs
// WCAG 2.2 contrast gate for the Cool Slate color system (light + dark).
// No deps: OKLCH -> linear sRGB -> WCAG relative luminance -> contrast ratio.
// Pair values MUST stay in sync with the tokens in src/assets/main.css.

function oklchToLinearSrgb(L, C, H) {
  const hr = (H * Math.PI) / 180
  const a = C * Math.cos(hr)
  const b = C * Math.sin(hr)
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b
  const s_ = L - 0.0894841775 * a - 1.2914855480 * b
  const l = l_ ** 3, m = m_ ** 3, s = s_ ** 3
  const r = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s
  const bl = -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s
  return [r, g, bl].map((v) => Math.min(1, Math.max(0, v)))
}
function luminance([L, C, H]) {
  const [r, g, b] = oklchToLinearSrgb(L, C, H)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(fg, bg) {
  const a = luminance(fg), b = luminance(bg)
  const [hi, lo] = a >= b ? [a, b] : [b, a]
  return (hi + 0.05) / (lo + 0.05)
}

// [label, fg(oklch L,C,H), bg(oklch L,C,H), minRatio]
const PAIRS = [
  ['sanity black/white',          [0, 0, 0],          [1, 0, 0],          20],
  // --- light ---
  ['L ink / canvas',              [0.22, 0.015, 240], [0.985, 0.005, 235], 4.5],
  ['L muted / canvas',            [0.48, 0.013, 240], [0.985, 0.005, 235], 4.5],
  ['L muted / card',              [0.48, 0.013, 240], [1, 0, 0],           4.5],
  ['L link tide-600 / card',      [0.47, 0.130, 205], [1, 0, 0],           4.5],
  ['L white / primary tide-600',  [1, 0, 0],          [0.47, 0.130, 205],  4.5],
  ['L white / destructive',       [1, 0, 0],          [0.51, 0.175, 22],   4.5],
  ['L sage-700 / sage-50',        [0.45, 0.105, 150], [0.95, 0.035, 150],  4.5],
  ['L amber-700 / amber-50',      [0.50, 0.090, 65],  [0.96, 0.045, 80],   4.5],
  ['L rose-700 / rose-50',        [0.46, 0.165, 22],  [0.95, 0.035, 20],   4.5],
  ['L info tide-700 / info-bg',   [0.40, 0.128, 205], [0.95, 0.030, 205],  4.5],
  ['L focus tide-500 / canvas',   [0.55, 0.118, 205], [0.985, 0.005, 235], 3.0],
  // --- dark ---
  ['D ink / canvas',              [0.95, 0.005, 240], [0.17, 0.012, 240],  4.5],
  ['D muted / canvas',            [0.68, 0.010, 240], [0.17, 0.012, 240],  4.5],
  ['D link / canvas',             [0.80, 0.090, 205], [0.17, 0.012, 240],  4.5],
  ['D link / card',               [0.80, 0.090, 205], [0.205, 0.013, 240], 4.5],
  ['D darklabel / primary',       [0.17, 0.012, 240], [0.72, 0.120, 200],  4.5],
  ['D darklabel / destructive',   [0.16, 0.010, 22],  [0.64, 0.175, 22],   4.5],
  ['D sage text / tint',          [0.80, 0.100, 150], [0.27, 0.050, 150],  4.5],
  ['D amber text / tint',         [0.84, 0.110, 80],  [0.29, 0.050, 70],   4.5],
  ['D rose text / tint',          [0.76, 0.140, 22],  [0.28, 0.070, 20],   4.5],
  ['D info text / tint',          [0.80, 0.090, 205], [0.27, 0.050, 205],  4.5],
  ['D focus / card',              [0.72, 0.110, 205], [0.205, 0.013, 240], 3.0],
]

let failed = 0
for (const [label, fg, bg, min] of PAIRS) {
  const ratio = contrast(fg, bg)
  const ok = ratio >= min
  if (!ok) failed++
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${ratio.toFixed(2)}:1  (min ${min.toFixed(1)})  ${label}`)
}
console.log(`\n${PAIRS.length - failed}/${PAIRS.length} pairs pass`)
if (failed) {
  console.error(`\n${failed} pair(s) below threshold — adjust the token (lower L by ~0.02 for text-on-light, or raise L for text-on-dark) in BOTH this file and main.css, then re-run.`)
  process.exit(1)
}
