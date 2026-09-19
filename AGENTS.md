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
- Commit messages end with the implementing agent's own truthful
  `Co-Authored-By: Name <email>` trailer; do not copy another model's identity.
- Reuse the board's area labels: `area:audit`, `area:authn`,
  `area:dashboard`, `area:db`, `area:docs`, `area:federation`,
  `area:protocol`, and `area:server`. Check the current board before adding
  a new area. Labels describe scope; assignees record responsibility.

## Build and verification

- [TOOLING.md](TOOLING.md) is the build and dependency source of truth. Use
  `mise run <task>` and the pinned tools. In `dashboard`, install with
  `pnpm install --frozen-lockfile`.
- Edit database queries in `db/queries` and regenerate `pkg/db` with
  `mise exec -- sqlc generate`; do not hand-edit generated query code.
- Run the relevant gates: `mise run ci:go` for Go changes,
  `mise run ci:frontend` for dashboard changes, and `mise run ci:smoke`
  for API/authentication behavior against the real test database. Use
  [TOOLING.md](TOOLING.md) for release/workflow checks when those files change.
- Verification must state what behavior was exercised and at which revision.
  Confirm that targeted tests actually ran: `go test -run` can succeed with
  no matching tests. Inspect failures and asynchronous test errors.
- Check affected user flows in a browser when layout or interaction changes;
  include relevant narrow-screen behavior.

## Dashboard and generated assets

- Follow the current HeroUI official documentation and preserve its default
  component styles. The former `DESIGN.md` is retired.
- Keep the approved HeroUI Theme Builder configuration:
  `chroma=0.1&hue=204&lightness=0.52&formRadius=small&radius=extra-small&base=0.003`.
  Its exported theme variables belong in `dashboard/src/styles/theme.css`,
  imported after `@heroui/styles`; preserve other component defaults.
- Prefer compositions of HeroUI components whenever they cover the UI need;
  use `Alert` for notices such as the frontend-rebuild banner.
- For an Alert with a single message, use `Alert.Title` alone inside
  `Alert.Content`. Add `Alert.Description` only for supplementary text.
- Write custom layout and styling as Tailwind classes directly in JSX.
  Reuse styles through React components in `dashboard/src/components/custom`,
  using `class-variance-authority` for shared variants.
- HTML mockups describe wireframes and interaction flows only. Implement visual
  details according to HeroUI rather than copying mockup styling.
- `dashboard-old` is a reference-only archive. Keep it outside imports, builds,
  tests and lint scopes; do not restore its routes or vendored UI into the app.
- M1 serves a bilingual foundation preview. Login, enrollment, self-service and
  admin pages are temporarily unavailable; backend APIs remain unchanged.
- The embedded bundle is generated, not committed. `pkg/webui/dist` is ignored
  apart from the tracked `.gitkeep` that keeps `go:embed all:dist` compiling on
  a clean checkout. After changing dashboard source run `mise run ci:frontend`;
  do not add anything under `pkg/webui/dist` to a commit.
- During integration, resolve source changes. Generated assets cannot conflict
  because they are not in the repository; when you need the merged source in a
  binary, force a rebuild with `mise run --force build:web`.

## User-facing language

- Follow the project's `srh` writing skill when it is available. It is not
  vendored here; do not claim to have applied unavailable instructions.
- Use plain language that says what happened and what to do next, without jargon
  or blame. Explain technical details only when they help the reader decide or act.
- Keep Chinese and English locale entries in sync. Public repository guidance
  describes roles and workflow without naming individual operators.
