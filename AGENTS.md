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
  `mise run <task>` and the pinned tools. Frontend dependencies use `npm ci`.
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

- Follow [DESIGN.md](DESIGN.md) and
  [the UI component rules](dashboard/src/components/ui/README.md).
  `dashboard/src/components/ui` is vendored: do not hand-edit it. Put
  application-specific components in `dashboard/src/components/custom`.
- After changing dashboard source, run `mise run build:web` and commit
  `pkg/webui/dist` in a separate `build: refresh embedded dashboard bundle`
  commit. `ci:frontend` checks the generated bundle against the committed one.
- During integration, resolve source changes first. Do not manually reconcile
  minified, content-hashed assets in `pkg/webui/dist`. Use a complete side as
  a temporary conflict resolution, then force a rebuild with
  `mise run --force build:web` from the merged source and commit the freshly
  generated bundle. Check for stale assets and run `mise run ci:frontend`.
  No custom merge driver is assumed.

## User-facing language

- Follow the project's `srh` writing skill when it is available. It is not
  vendored here; do not claim to have applied unavailable instructions.
- The repository's baseline is [DESIGN.md](DESIGN.md): plain language that says
  what happened and what to do next, without jargon or blame. Explain technical
  details only when they help the user decide or act.
- Keep Chinese and English locale entries in sync. Public repository guidance
  describes roles and workflow without naming individual operators.
