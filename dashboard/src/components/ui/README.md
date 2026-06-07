# ui/ — Vendored shadcn-vue Primitives

Vendored shadcn-vue primitives (new-york style, Reka UI under the hood).

**Do NOT hand-edit markup in this directory.** Restyle via tokens in
`src/assets/main.css`. Re-sync with:

```bash
npx shadcn-vue add <name> --overwrite
```

Bespoke first-party components live in `../custom/`.

---

## Alias Fence

The CLI is configured via `components.json` to write ALL vendored primitives
under `@/components/ui` (`src/components/ui/`). Nothing from the shadcn-vue
registry should land in `../custom/` or anywhere else. If `npx shadcn-vue add`
writes outside this directory, something is wrong with `components.json`.

---

## Token Wiring

Primitives are styled purely via the CSS custom properties defined in
`src/assets/main.css`. The shadcn-vue semantic variable names (`--primary`,
`--ring`, `--destructive`, etc.) are aliased there onto the Welcoming Vault
OKLCH tokens.

Overriding a visual role means updating the alias in `main.css`, not editing
the component's class list.

---

## Accent / Scarce Accent Rule

**`--accent` is mapped to `--color-sunken` (a barely-off-white neutral tint),
NOT to `--color-ember`.**

Rationale: shadcn-vue uses `--accent` as the hover background on ghost buttons,
outline buttons, and command/menu items. Mapping `--accent` to Ember would
cause every ghost-button hover to flash warm-orange, violating DESIGN.md's
Scarce Accent Rule ("Ember appears on no more than a couple of elements per
screen; its rarity is the point; an Ember used for decoration is a bug").

Ember is injected only as a deliberate one-off class on brand-mark elements
(the logo on the login/enroll card, the enrollment welcome moment). It is never
a system-level hover color.

---

## Components Present

| Directory | Primitive(s)                               |
|-----------|--------------------------------------------|
| `button/` | `Button` (variants: default/ghost/outline/secondary/destructive/link) |
| `input/`  | `Input`                                    |
| `label/`  | `Label`                                    |
| `card/`   | `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter`, `CardAction` |
| `dialog/` | `Dialog`, `DialogTrigger`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogDescription`, `DialogFooter`, `DialogClose`, `DialogOverlay`, `DialogScrollContent` |
| `badge/`  | `Badge` (variants: default/secondary/destructive/outline) |
| `alert/`  | `Alert`, `AlertTitle`, `AlertDescription`  |
