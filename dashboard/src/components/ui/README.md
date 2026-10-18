# coss ui components

Vendored from the coss ui registry (`apps/ui/registry/default/ui`, with `lib/` and
`hooks/` beside it) of <https://github.com/cosscom/ui>, at commit
`a597c6306a1c60e9f11e22e38bdf4a15f624b0f2`. `composer.tsx` is not copied.

Only the imports were rewritten (`@/registry/default/ui|lib|hooks` to
`@/components/ui`, `@/lib`, `@/hooks`) and `"use client"` removed. These files are
updated only by re-copying from the registry, never edited by hand. Licensing
follows coss's `LICENSING.md`.
