# Repository conventions

## Task scope and delivery

- Read the card's current discussion, decisions, dependencies and ownership before
  taking work. Keep material progress and verification on the card.
- Use branches named `{feat,fix,docs,chore,test,refactor}/phb-N-slug`, choosing one
  prefix and the card number. Use the agreed integration baseline; do not assume a
  branch named develop exists.
- Deliver implementation work at **Ready to Ship**, with a pushed branch and
  verified commit identified. Integration and deployment follow the project's
  separately authorized workflow.
- Reuse the board's area labels: `area:audit`, `area:authn`,
  `area:dashboard`, `area:db`, `area:docs`, `area:federation`,
  `area:protocol`, and `area:server`. Check the current board before adding
  a new area. Labels describe scope; assignees record responsibility.

## Build and verification

- [docs/tooling.md](docs/tooling.md) is the build and dependency source of truth. Use
  `mise run <task>` and the pinned tools. In `dashboard`, install with
  `pnpm install --frozen-lockfile`.
- Edit database queries in `db/queries` and regenerate `pkg/db` with
  `mise exec -- sqlc generate`; do not hand-edit generated query code.
- Run the relevant gates: `mise run ci:go` for Go changes,
  `mise run ci:frontend` for dashboard changes, and `mise run ci:smoke`
  for API/authentication behavior against the real test database. Use
  [docs/tooling.md](docs/tooling.md) for release/workflow checks when those files change.
- Verification must state what behavior was exercised and at which revision.
  Confirm that targeted tests actually ran: `go test -run` can succeed with
  no matching tests. Inspect failures and asynchronous test errors.
- Check affected user flows in a browser when layout or interaction changes;
  include relevant narrow-screen behavior. Dashboard walkthroughs run against
  the devtools API mock (`dashboard/src/devtools/mock`, open the TanStack
  Devtools panel): it switches the signed-in role between admin and member and
  sets how many accounts, user groups and invitations exist, so role guards and
  list states can be reviewed without a backend. See `dashboard/AGENTS.md` for
  what the panel covers.

## Dashboard and generated assets

- Shared components in `dashboard/src/components/custom` are built on the coss
  components in `dashboard/src/components/ui` (vendored: updated by
  re-copying from the coss registry, per `dashboard/src/components/ui/README.md`). New dialogs, alerts and one-time-code fields use the coss
  primitives through those shared components. The console is layered: canvas,
  inset panel, then `Frame`/`FramePanel` for blocks; no gradients or decoration;
  keep the `data-slot` hooks when restyling. Components come from
  `dashboard/src/components/ui` and the shared wrappers; no other UI library.
- A notice is `SurfaceAlert` (a coss `Alert` with `AlertTitle`, and
  `AlertDescription` only for supplementary text). A dialog is
  `@/components/ui/dialog` (`DialogHeader`, `DialogPanel`, `DialogFooter`).
- Buttons in the app come from `Button` in
  `dashboard/src/components/custom/Button.tsx`: coss's button (its `variant`,
  `size` and `render`) plus a `pending` prop. A pending button draws the
  spinner itself, keeps its text beside it (an icon-only button shows the
  spinner in place of its icon), stays focusable and is `aria-disabled`. Mark
  work in flight with `pending` and do not put a `Spinner` inside a button.
  Forms keep using `form.SubmitButton`, which builds on the same wrapper.
- Write custom layout and styling as Tailwind classes directly in JSX.
  Reuse styles through React components in `dashboard/src/components/custom`,
  using `class-variance-authority` for shared variants.
- HTML mockups describe wireframes and interaction flows only. Implement visual
  details with the coss components rather than copying mockup styling.
- The embedded bundle is generated, not committed. `pkg/webui/dist` is ignored
  apart from the tracked `.gitkeep` that keeps `go:embed all:dist` compiling on
  a clean checkout. After changing dashboard source run `mise run ci:frontend`;
  do not add anything under `pkg/webui/dist` to a commit.
- During integration, resolve source changes. Generated assets cannot conflict
  because they are not in the repository; when you need the merged source in a
  binary, force a rebuild with `mise run --force build:web`.
- After an async mutation completes (success or failure), await route navigation
  before proceeding. Use `await navigate(...)` in `onSuccess` or `onFailure`
  handlers, not `void navigate(...)`. This ensures the route loads before the
  handler returns.

## User-facing language

- Follow the project's `srh` writing skill when it is available. It is not
  vendored here; do not claim to have applied unavailable instructions.
- Use plain language that says what happened and what to do next, without jargon
  or blame. Explain technical details only when they help the reader decide or act.
- Keep Chinese and English locale entries in sync. Public repository guidance
  describes roles and workflow without naming individual operators.
