# coss ui components

Vendored from the coss ui registry (`apps/ui/registry/default/ui`, with `lib/` and
`hooks/` beside it) of <https://github.com/cosscom/ui>, at commit
`a597c6306a1c60e9f11e22e38bdf4a15f624b0f2`. `composer.tsx` is not copied.

## Re-copying

To reproduce the vendored state: copy the files from the registry, rewrite the
imports (`@/registry/default/ui|lib|hooks` to `@/components/ui`, `@/lib`,
`@/hooks`), drop `"use client"`, then run from `dashboard/`:

```sh
pnpm exec biome check --write src/components/ui src/lib src/hooks
```

(biome reorders imports and export specifiers and reformats). Do not edit these
files by hand.

## Licences

### MIT: components, lib, hooks

`src/components/ui`, `src/lib/utils.ts`, `src/lib/segmented-control.ts`,
`src/hooks/use-media-query.ts` and `src/hooks/use-copy-to-clipboard.ts` come from
`apps/ui/` upstream, which is MIT licensed:

    MIT License

    Copyright (c) 2025 coss.com
    Originally Copyright (c) 2025 Origin UI

    Permission is hereby granted, free of charge, to any person obtaining a copy
    of this software and associated documentation files (the "Software"), to deal
    in the Software without restriction, including without limitation the rights
    to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
    copies of the Software, and to permit persons to whom the Software is
    furnished to do so, subject to the following conditions:

    The above copyright notice and this permission notice shall be included in all
    copies or substantial portions of the Software.

    THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
    IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
    FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
    AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
    LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
    OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
    SOFTWARE.

### AGPL-3.0: `src/styles/coss.css`

`src/styles/coss.css` is derived from `packages/ui/src/styles/globals.css` in the
same commit. That path is outside the MIT directories (`apps/origin/`, `apps/ui/`),
so it falls under the repository's default licence, AGPL-3.0 (see upstream
`LICENSING.md`).

### Fonts

`src/styles/fonts/CalSansVF.woff2` and `PaperMono-Regular.woff2` come from
`packages/ui/src/fonts/` in the same commit and keep their own licences:

- Cal Sans, <https://github.com/calcom/sans>: SIL Open Font License 1.1.
- Paper Mono, <https://github.com/paper-design/paper-mono>: SIL Open Font License 1.1.
