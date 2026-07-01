# Release CI Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the existing tag-driven GoReleaser+ko release pipeline to 2026 GitHub Actions supply-chain best practice — SHA-pinned actions, least-privilege permissions, a release concurrency guard, native SLSA build-provenance attestations, a runner-egress audit, and a pre-tag validation gate — without changing the release architecture.

**Architecture:** Keep GoReleaser + ko exactly as-is (static Go binary with embedded SPA → multi-arch OCI to GHCR + SBOMs + checksums + cosign signatures). Change only the GitHub Actions surface (`release.yml`, `ci.yml`, a new path-filtered `release-dryrun.yml`), add `actions/attest` provenance steps, add `docker_digest` output for image attestation, and wire lint/validation through pinned `mise` tasks.

**Tech Stack:** GitHub Actions, GoReleaser 2.16.0 + ko, cosign 3.1.1, `actions/attest@v4`, `step-security/harden-runner`, `actionlint`, `zizmor`, mise (aqua backend).

**Spec:** `docs/superpowers/specs/2026-07-01-release-ci-hardening-design.md`

## Resolved action pins (use verbatim)

Existing actions are pinned to the latest patch **within their current major** (behavior-neutral hardening — no major bumps):

| Action | Pin |
|---|---|
| `actions/checkout` | `df4cb1c069e1874edd31b4311f1884172cec0e10` # v6.0.3 |
| `jdx/mise-action` | `5228313ee0372e111a38da051671ca30fc5a96db` # v3.6.3 |
| `docker/login-action` | `c94ce9fb468520275223c153574b00df6fe4bcc9` # v3.7.0 |
| `actions/attest` (new) | `a1948c3f048ba23858d222213b7c278aabede763` # v4.1.1 |
| `step-security/harden-runner` (new) | `9af89fc71515a100421586dfdb3dc9c984fbf411` # v2.19.4 |

> Major upgrades (checkout v7, mise-action v4, login-action v4 all exist) are intentionally out of scope; leave them to Dependabot, validated by the `release-dryrun` job.

## File structure

- **Modify** `mise.toml` — pin `actionlint` + `zizmor`; add `ci:release-check`, `ci:lint-actions`, `ci:release-snapshot` tasks.
- **Regenerate** `mise.lock` — via `mise install` (locks the two new tools).
- **Modify** `.goreleaser.yaml` — add `docker_digest` block (emits `dist/digests.txt` for image attestation).
- **Modify** `.github/workflows/release.yml` — concurrency guard, SHA pins, job-scoped permissions, harden-runner, two `actions/attest` steps.
- **Create** `.github/zizmor.yml` — document-ignore the deferred-Low `artipacked` (persist-credentials) finding.
- **Modify** `.github/workflows/ci.yml` — SHA pins + harden-runner on existing jobs; new `release-check` job (goreleaser check + workflow lint).
- **Create** `.github/workflows/release-dryrun.yml` — path-filtered full snapshot build (no publish).
- **Modify** `TOOLING.md`, `README.md` — document the gate, provenance, consumer verification, and deferred items.

---

### Task 0: Pin lint toolchain and add release/lint mise tasks

**Goal:** `actionlint` and `zizmor` are pinned in `mise.lock`, and the three CI wrapper tasks exist so every later task (and CI) runs the same commands.

**Files:**
- Modify: `mise.toml` (`[tools]` block ~line 10; add task blocks after `ci:smoke`, ~line 163)
- Regenerate: `mise.lock`

**Acceptance Criteria:**
- [ ] `mise install` succeeds and `mise.lock` gains `aqua:rhysd/actionlint` and `aqua:zizmorcore/zizmor` entries.
- [ ] `mise exec -- actionlint --version` and `mise exec -- zizmor --version` print versions.
- [ ] `mise run ci:release-check` passes (the current `.goreleaser.yaml` is already valid).
- [ ] `mise tasks` lists `ci:release-check`, `ci:lint-actions`, `ci:release-snapshot`.

**Verify:** `mise install && mise run ci:release-check` → exits 0; `mise exec -- zizmor --version` → prints `zizmor 1.x`.

**Steps:**

- [ ] **Step 1: Add the two tools to `[tools]` in `mise.toml`.** After the `sigstore/cosign` line (line 10), add:

```toml
# Workflow linting (used by `mise run ci:lint-actions` and the CI release-check job):
# actionlint = schema + shellcheck of workflow YAML; zizmor = Actions security audit.
"aqua:rhysd/actionlint" = "1.7.12"
"aqua:zizmorcore/zizmor" = "1.26.1"
```

- [ ] **Step 2: Add the three tasks** to the `ci:` section of `mise.toml` (immediately after the `[tasks."ci:smoke"]` block, ~line 163):

```toml
[tasks."ci:release-check"]
description = "CI: validate the GoReleaser release config (`goreleaser check`) — cheap, catches a broken .goreleaser.yaml before a tag is cut."
run = "goreleaser check"

[tasks."ci:lint-actions"]
description = "CI: lint GitHub Actions workflows — actionlint (schema/shellcheck) + zizmor (supply-chain security audit)."
run = """
set -e
actionlint
zizmor .github/workflows
"""

[tasks."ci:release-snapshot"]
description = "CI: full GoReleaser+ko build with NO publish (release dry-run) — proves the release still builds. The release-dryrun job runs this; also the local dry-run."
run = "goreleaser release --snapshot --clean"
```

- [ ] **Step 3: Install + lock.** Run:

```bash
mise install
```

Expected: installs actionlint + zizmor; `mise.lock` is modified (new tool entries + checksums).

- [ ] **Step 4: Verify tools + config check.** Run:

```bash
mise exec -- actionlint --version
mise exec -- zizmor --version
mise run ci:release-check
```

Expected: both versions print; `goreleaser check` prints `config is valid` and exits 0.

> Do **not** run `mise run ci:lint-actions` yet — it will report `unpinned-uses` on the not-yet-hardened workflows. That gate is expected to pass only after Tasks 2–3.

- [ ] **Step 5: Commit.**

```bash
git add mise.toml mise.lock
git commit -m "build(mise): pin actionlint + zizmor; add release-check/lint/snapshot tasks"
```

---

### Task 1: Add `docker_digest` and validate the release build locally

**Goal:** GoReleaser emits `dist/digests.txt` for image attestation, and a local snapshot proves the whole pipeline builds. This is where the one real unknown — whether ko image digests are captured — gets settled empirically.

**Files:**
- Modify: `.goreleaser.yaml` (after the `checksum:` block, ~line 64)

**Acceptance Criteria:**
- [ ] `.goreleaser.yaml` has a `docker_digest` block with `name_template: "digests.txt"`.
- [ ] `mise run ci:release-check` still passes (config valid).
- [ ] `mise run ci:release-snapshot` completes successfully (SPA build → multi-arch Go build → ko image assembly → archives → checksums → SBOMs).
- [ ] `dist/artifacts.json` contains a `Docker Manifest` entry whose `path` is `ghcr.io/tundrawork/prohibitorum...@sha256:...` (confirms ko digests are recorded — the attestation subject source exists).

**Verify:** `mise run ci:release-snapshot` → exits 0; `jq -r '.[] | select(.type=="Docker Manifest") | .path' dist/artifacts.json` → prints at least one `ghcr.io/tundrawork/prohibitorum@sha256:...` line.

**Steps:**

- [ ] **Step 1: Add the `docker_digest` block** to `.goreleaser.yaml`, immediately after the `checksum:` block (introduced in GoReleaser v2.12; we run 2.16.0):

```yaml
# Emit dist/digests.txt (image + manifest digests) so the release workflow can
# hand it to actions/attest for SLSA build provenance on the OCI images.
docker_digest:
  name_template: "digests.txt"
```

- [ ] **Step 2: Validate config.** Run:

```bash
mise run ci:release-check
```

Expected: `config is valid`, exit 0. (If it errors on `docker_digest`, confirm the installed GoReleaser is ≥ 2.12 with `mise exec -- goreleaser --version`.)

- [ ] **Step 3: Full local dry-run.** Run:

```bash
mise run ci:release-snapshot
```

Expected: completes; artifacts appear under `./dist/` (gitignored). ko builds images without pushing (no Docker daemon required — ko assembles OCI layers directly).

- [ ] **Step 4: Confirm ko image digests are recorded.** Run:

```bash
jq -r '.[] | select(.type=="Docker Manifest" or .type=="Docker Image") | "\(.type)\t\(.path)"' dist/artifacts.json
ls -l dist/digests.txt 2>/dev/null || echo "digests.txt not produced in snapshot (expected — it is publish-gated)"
```

Expected: at least one `Docker Manifest` line with `ghcr.io/tundrawork/prohibitorum@sha256:...`. `digests.txt` may be absent in snapshot because it lists *published* digests — that is fine; `release.yml` only runs on real tags where images are pushed. The `artifacts.json` presence confirms the fallback source (see Task 2 contingency) exists.

- [ ] **Step 5: Commit.**

```bash
git add .goreleaser.yaml
git commit -m "ci(release): emit docker digests file for image attestation"
```

---

### Task 2: Harden `release.yml` and add zizmor config

**Goal:** `release.yml` follows least-privilege + SHA-pinning best practice, guards against concurrent releases, audits runner egress, and produces SLSA build-provenance attestations for binaries and images.

**Files:**
- Modify: `.github/workflows/release.yml` (full rewrite)
- Create: `.github/zizmor.yml`

**Acceptance Criteria:**
- [ ] Every `uses:` in `release.yml` is pinned to a full commit SHA with a `# vX.Y.Z` comment.
- [ ] Top-level `permissions: {}`; the `release` job grants only `contents: write`, `packages: write`, `id-token: write`, `attestations: write`.
- [ ] `concurrency:` guard present with `cancel-in-progress: false`.
- [ ] `step-security/harden-runner` is the first step (egress audit).
- [ ] Two `actions/attest@v4` steps run after `mise run prod:release` (checksums + digests).
- [ ] `mise exec -- actionlint .github/workflows/release.yml` is clean; `mise exec -- zizmor .github/workflows/release.yml` reports no findings other than the documented-ignored `artipacked`.

**Verify:** `mise exec -- actionlint .github/workflows/release.yml && mise exec -- zizmor .github/workflows/release.yml` → exit 0.

**Steps:**

- [ ] **Step 1: Replace `.github/workflows/release.yml`** with:

```yaml
name: release

# Tag-driven release: GoReleaser + ko build the binaries + multi-arch OCI images
# (ghcr.io/tundrawork/prohibitorum), SBOMs, checksums, cosign signatures, and
# GitHub-native SLSA build-provenance attestations. See TOOLING.md §Prod.
on:
  push:
    tags: ["v*"]

# One in-flight release per tag; never cancel a partially-published release.
concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false

# Least privilege: no ambient token scopes; the release job grants exactly what it needs.
permissions: {}

env:
  MISE_EXPERIMENTAL: true

jobs:
  release:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    permissions:
      contents: write      # create the GitHub Release
      packages: write      # push images to GHCR
      id-token: write      # cosign keyless signing + attestation OIDC
      attestations: write  # persist SLSA build provenance
    steps:
      - name: Harden runner (egress audit)
        uses: step-security/harden-runner@9af89fc71515a100421586dfdb3dc9c984fbf411 # v2.19.4
        with:
          egress-policy: audit
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
        with:
          fetch-depth: 0 # GoReleaser needs full history + tags
      - uses: jdx/mise-action@5228313ee0372e111a38da051671ca30fc5a96db # v3.6.3 — toolchain (go, node, goreleaser, cosign) from mise.lock
      - name: Log in to GHCR
        uses: docker/login-action@c94ce9fb468520275223c153574b00df6fe4bcc9 # v3.7.0
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Release (GoReleaser + ko)
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: mise run prod:release
      - name: Attest build provenance (binaries + archives)
        uses: actions/attest@a1948c3f048ba23858d222213b7c278aabede763 # v4.1.1
        with:
          subject-checksums: ./dist/checksums.txt
      - name: Attest build provenance (images)
        uses: actions/attest@a1948c3f048ba23858d222213b7c278aabede763 # v4.1.1
        with:
          subject-checksums: ./dist/digests.txt
```

> No `if:` guard on the image-attest step: this workflow only triggers on tags, so images are always pushed and `digests.txt` always produced.

- [ ] **Step 2: Create `.github/zizmor.yml`** to document-ignore the one deliberately-deferred Low-severity finding (`artipacked` = checkout without `persist-credentials: false`):

```yaml
# zizmor GitHub Actions security-audit config.
# See docs/superpowers/specs/2026-07-01-release-ci-hardening-design.md.
rules:
  # persist-credentials:false was consciously DEFERRED (Low severity) in the
  # hardening spec: GoReleaser authenticates via GITHUB_TOKEN in env, not the
  # checkout-persisted git credential, so residual risk is minimal. Revisit when
  # enabling the deferred Low items.
  artipacked:
    ignore:
      - release.yml
      - ci.yml
      - release-dryrun.yml
```

- [ ] **Step 3: Lint the workflow.** Run:

```bash
mise exec -- actionlint .github/workflows/release.yml
mise exec -- zizmor .github/workflows/release.yml
```

Expected: both exit 0. **If zizmor reports a genuine finding** (e.g. `cache-poisoning` on the mise-action step because the release publishes), fix it for real — add `cache: false` under the `jdx/mise-action` step's `with:` — rather than ignoring it. Only `artipacked` is ignore-listed.

- [ ] **Step 4: Contingency — only if a later real tag shows no image digests.** The primary path relies on GoReleaser's `docker_digest` covering ko images. If the first tagged release (Task 4 post-merge check) produces an empty/missing `dist/digests.txt`, replace the "images" attest step with a digest extracted from `artifacts.json` (a multi-arch tag resolves to the manifest-index digest — a single subject):

```yaml
      - name: Extract image manifest digest (fallback)
        id: img
        run: |
          path=$(jq -r '.[] | select(.type=="Docker Manifest") | .path' dist/artifacts.json | head -1)
          name=${path%@*}; name=${name%%:*}   # strip @sha256 and any :tag → bare repo
          echo "name=${name}" >> "$GITHUB_OUTPUT"
          echo "digest=${path#*@}" >> "$GITHUB_OUTPUT"
      - name: Attest build provenance (images)
        uses: actions/attest@a1948c3f048ba23858d222213b7c278aabede763 # v4.1.1
        with:
          subject-name: ${{ steps.img.outputs.name }}
          subject-digest: ${{ steps.img.outputs.digest }}
```

Leave the primary `subject-checksums: ./dist/digests.txt` step in place unless the contingency is actually needed.

- [ ] **Step 5: Commit.**

```bash
git add .github/workflows/release.yml .github/zizmor.yml
git commit -m "ci(release): SHA-pin actions, scope permissions, add harden-runner + SLSA attestations"
```

---

### Task 3: Harden `ci.yml` and add the pre-tag validation gate

**Goal:** The existing CI jobs are SHA-pinned and egress-audited; a fast `release-check` job lints the release config + workflows on every PR; a path-filtered `release-dryrun` workflow proves the release still builds when release-affecting files change.

**Files:**
- Modify: `.github/workflows/ci.yml`
- Create: `.github/workflows/release-dryrun.yml`

**Acceptance Criteria:**
- [ ] Every `uses:` in `ci.yml` and `release-dryrun.yml` is SHA-pinned with a version comment.
- [ ] Each job's first step is `step-security/harden-runner` (audit).
- [ ] `ci.yml` has a `release-check` job running `ci:release-check` + `ci:lint-actions`.
- [ ] `release-dryrun.yml` runs `ci:release-snapshot`, triggered by `workflow_dispatch` + `pull_request` on release-affecting paths only.
- [ ] `mise run ci:lint-actions` now passes clean across **all** workflows (everything pinned).

**Verify:** `mise run ci:lint-actions` → exit 0 (no findings beyond the ignored `artipacked`).

**Steps:**

- [ ] **Step 1: Replace `.github/workflows/ci.yml`** with (adds harden-runner + SHA pins to the existing `gate`/`smoke` jobs and a new `release-check` job):

```yaml
name: ci

on:
  workflow_dispatch:
  pull_request:
  push:
    branches: ["master"]

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: read

env:
  MISE_EXPERIMENTAL: true

jobs:
  # The same gate developers run locally: `mise run ci` (Go vet/build/test +
  # frontend install/test/typecheck + embedded-dist freshness guard).
  gate:
    name: Gate (Go + frontend)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - name: Harden runner (egress audit)
        uses: step-security/harden-runner@9af89fc71515a100421586dfdb3dc9c984fbf411 # v2.19.4
        with:
          egress-policy: audit
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
      - uses: jdx/mise-action@5228313ee0372e111a38da051671ca30fc5a96db # v3.6.3 — installs the mise.lock-pinned toolchain (auto --locked)
      - run: mise run ci

  # Validate the release config + workflows on every PR so a broken release
  # fails here, not on the first tag. Cheap (no build).
  release-check:
    name: Release config + workflow lint
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - name: Harden runner (egress audit)
        uses: step-security/harden-runner@9af89fc71515a100421586dfdb3dc9c984fbf411 # v2.19.4
        with:
          egress-policy: audit
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
      - uses: jdx/mise-action@5228313ee0372e111a38da051671ca30fc5a96db # v3.6.3
      - run: mise run ci:release-check
      - run: mise run ci:lint-actions

  # End-to-end smoke against a real server + the compose Postgres (scripts/db.sh
  # brings it up; the smoke talks to the DB via pgx).
  smoke:
    name: End-to-end smoke
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - name: Harden runner (egress audit)
        uses: step-security/harden-runner@9af89fc71515a100421586dfdb3dc9c984fbf411 # v2.19.4
        with:
          egress-policy: audit
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
      - uses: jdx/mise-action@5228313ee0372e111a38da051671ca30fc5a96db # v3.6.3
      # Bring the dev Postgres up via compose. ubuntu-latest ships both podman and
      # docker; pin docker compose here because podman's compose provider can be
      # flaky on the runner (local `mise run ci:smoke` stays auto-detect).
      - run: mise run ci:smoke
        env:
          PROHIBITORUM_COMPOSE: docker compose
```

- [ ] **Step 2: Create `.github/workflows/release-dryrun.yml`** (path-filtered full build, no publish):

```yaml
name: release-dryrun

# Full GoReleaser + ko build with NO publish, to prove the release still builds.
# Path-filtered to release-affecting changes so ordinary PRs stay fast.
on:
  workflow_dispatch:
  pull_request:
    paths:
      - ".goreleaser.yaml"
      - "mise.toml"
      - "mise.lock"
      - ".github/workflows/release.yml"
      - ".github/workflows/release-dryrun.yml"
      - "cmd/**"
      - "pkg/**"
      - "dashboard/**"
      - "go.mod"
      - "go.sum"

concurrency:
  group: release-dryrun-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: read

env:
  MISE_EXPERIMENTAL: true

jobs:
  snapshot:
    name: GoReleaser snapshot (build only)
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - name: Harden runner (egress audit)
        uses: step-security/harden-runner@9af89fc71515a100421586dfdb3dc9c984fbf411 # v2.19.4
        with:
          egress-policy: audit
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
        with:
          fetch-depth: 0
      - uses: jdx/mise-action@5228313ee0372e111a38da051671ca30fc5a96db # v3.6.3
      - name: Snapshot build (no publish)
        run: mise run ci:release-snapshot
```

- [ ] **Step 3: Lint all workflows.** Run:

```bash
mise run ci:lint-actions
```

Expected: exit 0. actionlint clean; zizmor reports nothing beyond the ignored `artipacked`. Fix any genuine zizmor finding for real (see Task 2 Step 3 note).

- [ ] **Step 4: Commit.**

```bash
git add .github/workflows/ci.yml .github/workflows/release-dryrun.yml
git commit -m "ci: SHA-pin + harden-runner all workflows; add release-check + path-filtered release-dryrun"
```

---

### Task 4: Document the release process, provenance, and verification

**Goal:** `TOOLING.md` and `README.md` describe the hardened pipeline: the pre-tag gate, SLSA provenance, consumer verification commands, the required manual GitHub settings, and the deferred Low-severity items.

**Files:**
- Modify: `TOOLING.md` (`## Prod` §, ~lines 37–47; `## CI` §, ~lines 49–54)
- Modify: `README.md` (Status/Security area)

**Acceptance Criteria:**
- [ ] `TOOLING.md` documents: SHA-pinning + least-privilege + harden-runner; `actions/attest` provenance (checksums + images); the `release-check` + `release-dryrun` gate; the deferred Low items (concurrency guard is now IN, so only `persist-credentials:false` + cosign `--bundle` remain deferred).
- [ ] `TOOLING.md` includes the **manual GitHub setup** checklist and **consumer verification** commands.
- [ ] `README.md` mentions signed images + build provenance with a one-line verify pointer.
- [ ] No milestone/version labels baked into prose (per repo convention).

**Verify:** `mise exec -- actionlint .github/workflows` still clean (docs-only change shouldn't affect it); manual read confirms commands match the workflows.

**Steps:**

- [ ] **Step 1: Update `TOOLING.md` `## Prod` section.** Extend the existing bullet list with the hardening + provenance additions and append the manual-steps + verification subsections:

```markdown
### Release workflow hardening

`.github/workflows/release.yml` follows current supply-chain best practice:
- **Actions SHA-pinned** to full commit SHAs (version in a trailing comment) — mutable tags can't be hijacked.
- **Least privilege** — top-level `permissions: {}`; the `release` job grants only `contents/packages/id-token/attestations: write`.
- **Concurrency guard** — one release per tag, never cancelled mid-publish.
- **`step-security/harden-runner`** (egress `audit`) as the first step. Flip to `block` with an allowlist once an audit run shows the real endpoints.
- **SLSA build provenance** via `actions/attest` for `dist/checksums.txt` (binaries/archives) and `dist/digests.txt` (images, emitted by GoReleaser `docker_digest`). Public repo → public-good Sigstore + Rekor.

### Manual GitHub setup (one-time, out-of-band)

1. Settings ▸ Actions ▸ General ▸ Workflow permissions → **Read repository contents and packages permissions** (read-only default; workflows grant writes per job).
2. GHCR needs no signup — it authenticates with `GITHUB_TOKEN`. The first tagged release creates `ghcr.io/tundrawork/prohibitorum`; afterward set the package visibility to **public** and confirm it's linked to the repo. Ensure the org doesn't block Actions from creating packages.
3. Cut a release by pushing an annotated `vX.Y.Z` tag. `prerelease: auto` flags pre-release tags (e.g. `v1.2.3-rc1`).
4. *(Optional)* Enforce SHA-pinning via the org/repo allowed-actions policy; install the StepSecurity app for the harden-runner egress dashboard.

### Verifying a release (consumers)

​```sh
# SLSA build provenance
gh attestation verify oci://ghcr.io/tundrawork/prohibitorum:<tag> --owner TundraWork
gh attestation verify prohibitorum_<version>_linux_amd64.tar.gz --owner TundraWork

# cosign keyless signature
cosign verify ghcr.io/tundrawork/prohibitorum:<tag> \
  --certificate-identity-regexp 'https://github.com/TundraWork/Prohibitorum/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
​```

### Deferred (Low severity)

Documented in the hardening spec, easy to enable later: `persist-credentials: false` on the release checkout, and cosign v3 `--bundle` single-file signatures.
```

- [ ] **Step 2: Update `TOOLING.md` `## CI` section** — add the `release-check` job and the `release-dryrun` workflow to the jobs description:

```markdown
- **release-check** runs `mise run ci:release-check` (`goreleaser check`) + `mise run ci:lint-actions` (`actionlint` + `zizmor` over `.github/workflows`) on every PR — a broken release config or workflow fails here, not on the first tag.
- **release-dryrun** (`.github/workflows/release-dryrun.yml`) runs `mise run ci:release-snapshot` (`goreleaser release --snapshot --clean`, full multi-arch build, no publish) — path-filtered to release-affecting changes + `workflow_dispatch`, so ordinary PRs stay fast.
```

- [ ] **Step 3: Update `README.md`** — under the Status/Security area, add one line:

```markdown
- **Signed, provenance-tracked releases** — multi-arch OCI images to GHCR with cosign signatures, SBOMs, and SLSA build provenance (`gh attestation verify oci://ghcr.io/tundrawork/prohibitorum:<tag> --owner TundraWork`). See TOOLING.md §Prod.
```

- [ ] **Step 4: Sanity-check + commit.** Run:

```bash
mise exec -- actionlint .github/workflows
git add TOOLING.md README.md
git commit -m "docs: document release hardening, SLSA provenance, and verification"
```

- [ ] **Step 5 (post-merge, manual): first-release validation.** After merging, push a test pre-release tag (e.g. `v0.0.1-rc1`) and confirm: the `release` workflow succeeds; `gh attestation verify oci://ghcr.io/tundrawork/prohibitorum:v0.0.1-rc1 --owner TundraWork` passes. If `dist/digests.txt` was empty (ko not covered by `docker_digest`), apply the Task 2 Step 4 contingency and re-tag.

---

## Notes for the implementer

- **Run tasks in order** — Task 0 provides the tooling every later task's verify step uses.
- **zizmor findings:** only `artipacked` (the deferred `persist-credentials`) is ignore-listed. Any other finding is a real issue — fix it (e.g. `cache: false` on mise-action if `cache-poisoning` fires) rather than expanding the ignore list.
- **The ko-digest question is the one genuine unknown.** Task 1 confirms `artifacts.json` records ko digests (fallback source); the definitive `digests.txt` check is the first real tag (Task 4 Step 5). The contingency is ready.
- **No major action bumps** — pins stay within the current major; Dependabot + `release-dryrun` handle upgrades later.
- **Commit style:** conventional commits, no `Co-Authored-By` trailer (repo convention).
