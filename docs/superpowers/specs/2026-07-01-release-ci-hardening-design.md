# Release CI hardening — design

Date: 2026-07-01
Status: approved (pending spec review)

## Goal

Harden the existing tag-driven release pipeline (`.github/workflows/release.yml`
+ `.goreleaser.yaml`) to current GitHub Actions supply-chain best practice,
**without changing the GoReleaser + ko architecture**. Add verifiable SLSA build
provenance and a pre-tag validation gate so the first real tag doesn't fail.

Constraints:

- Public repo (`github.com/TundraWork/Prohibitorum`), images to
  `ghcr.io/tundrawork/prohibitorum`.
- Release binaries stay **linux-only** (`amd64`, `arm64`) — it's a server daemon
  shipped as a container/linux binary.
- The pipeline exists but **has never run** (no tags yet), so validation before
  tagging is a real safety requirement, not a nicety.

## Background — what exists today

- `.github/workflows/release.yml`: on `push` tag `v*` → mise toolchain → GHCR
  login → `mise run prod:release` (= `goreleaser release --clean`).
- `.goreleaser.yaml`: static Go build (CGO off, `-trimpath`, `-tags=nodynamic`,
  `-s -w`, `mod_timestamp`), ko multi-arch OCI (chainguard static base, SPDX
  SBOM, OCI labels), tar.gz archives, `checksums.txt`, keyless cosign signing of
  checksums (`sign-blob`) and images (`docker_signs`), GitHub Release.
- Tooling pinned in `mise.lock`: GoReleaser `2.16.0`, cosign `3.1.1`.

The architecture is sound. The gaps are all in the GitHub Actions surface and in
supply-chain verifiability.

## Gap analysis (research-backed)

| Gap | Today | Best practice | Severity | In scope |
|---|---|---|---|---|
| Actions pinned to mutable tags | `@v6`, `@v3` | Full commit SHA + version comment | High | ✅ |
| Broad workflow-level permissions | all writes at top of `release.yml` | Scope writes to the job that needs them | Medium | ✅ |
| No SLSA build provenance | cosign sigs only | Native `actions/attest@v4` for binaries + images | Medium-High | ✅ |
| Release config never validated | none | `goreleaser check` + snapshot dry-run + `actionlint` + `zizmor` in CI | Medium | ✅ |
| No runner egress visibility | none | `step-security/harden-runner` (audit) | Medium | ✅ |
| Release concurrency race | none | `concurrency:` guard | Low | ❌ deferred |
| Checkout persists credentials | default | `persist-credentials: false` | Low | ❌ deferred |
| cosign v2-style sign outputs | separate cert/sig | cosign v3 `--bundle` (`.sigstore.json`) | Low | ❌ deferred |

Deferred Low-severity items are documented in [Out of scope](#out-of-scope) so
they're trivial to enable later.

## Design

### 1. `.github/workflows/release.yml`

- **SHA-pin all actions** to a full commit SHA, each with a `# vX.Y.Z` comment so
  the human-readable version and Dependabot updates still work. Actions:
  `actions/checkout`, `jdx/mise-action`, `docker/login-action`,
  `actions/attest` (new), `step-security/harden-runner` (new). SHAs are resolved
  at implementation time from each action's tagged release.
- **Least-privilege permissions:** top-level `permissions: {}`; grant writes only
  on the `release` job:

  ```yaml
  permissions:
    contents: write      # create the GitHub Release
    packages: write       # push images to GHCR
    id-token: write       # cosign keyless + attestation OIDC
    attestations: write   # persist SLSA provenance
  ```

- **harden-runner** as the **first** step of the job (must precede checkout to
  capture egress), `egress-policy: audit`. Blocking with an allowlist is a
  follow-up once an audit run reveals the real endpoints (Fulcio, Rekor, ghcr.io,
  api.github.com, aqua/mise download hosts, npm registry, Go module proxy).
- After `mise run prod:release`, two provenance steps (GoReleaser leaves outputs
  in `./dist`):

  ```yaml
  - uses: actions/attest@<sha>   # v4.x — binaries + archives
    with:
      subject-checksums: ./dist/checksums.txt
  - uses: actions/attest@<sha>   # v4.x — ko images
    if: startsWith(github.ref, 'refs/tags/v')
    with:
      subject-checksums: ./dist/digests.txt
  ```

### 2. `.goreleaser.yaml`

Single addition so ko image digests are emitted in a predictable file for the
image attestation (introduced in GoReleaser v2.12; we run 2.16.0):

```yaml
docker_digest:
  name_template: "digests.txt"
```

`checksum.name_template` is already `checksums.txt`. **No other changes** — ko,
SBOM, and cosign signing config are untouched.

### 3. `.github/workflows/ci.yml` — pre-tag validation gate

Two additions to the existing `ci.yml` (keeps `permissions: contents: read`):

- **`release-check` job** (runs on every PR/push): harden-runner (audit) →
  checkout → mise → `mise run ci:release-check` (`goreleaser check`) +
  `mise run ci:lint-actions` (`actionlint` + `zizmor` over `.github/workflows`).
  Fast; catches a broken `.goreleaser.yaml` or workflow before a tag is cut.
- **Snapshot dry-run** — `goreleaser release --snapshot --clean` (full SPA +
  multi-arch + image assembly, no publish). Expensive, so **path-filtered** to
  release-affecting files (`.goreleaser.yaml`, `mise.toml`, `mise.lock`,
  `.github/workflows/**`, `cmd/**`, `pkg/**`) plus `workflow_dispatch`. Normal
  PRs stay fast; release-affecting PRs prove the release still builds.

### 4. `mise.toml`

- Pin `actionlint` and `zizmor` via aqua alongside the existing goreleaser/cosign
  pins (fall back to `uvx zizmor` only if no aqua package exists).
- Add tasks:
  - `ci:release-check` → `goreleaser check`
  - `ci:lint-actions` → `actionlint` + `zizmor .github/workflows`
  - `ci:release-snapshot` → `goreleaser release --snapshot --clean` (used by the
    path-filtered CI job and for local dry-runs)

## Manual / out-of-band steps

These are not code changes; they are yours to perform (documented in the plan and
surfaced when relevant):

1. **Default token permissions** → Settings ▸ Actions ▸ General ▸ Workflow
   permissions = **Read repository contents and packages permissions**
   (read-only). Our workflows grant writes explicitly per job.
2. **GHCR package** — no registry signup; GHCR authenticates with
   `GITHUB_TOKEN`. The first tagged release auto-creates
   `ghcr.io/tundrawork/prohibitorum`. Afterward: set the package visibility to
   **public** (to match the repo) and confirm it's linked to the repo with
   Actions write access. Verify the org doesn't block Actions from creating
   packages.
3. **Cut a release** — push an annotated `vX.Y.Z` tag; that triggers
   `release.yml`. `prerelease: auto` marks pre-release tags (e.g. `v1.2.3-rc1`).
4. *(Optional)* Enforce SHA-pinning via org/repo allowed-actions policy
   (available since Aug 2025).
5. *(Optional)* Install the StepSecurity GitHub App for the harden-runner egress
   dashboard (not needed for audit mode).

### Consumer verification (documented in README/TOOLING)

```sh
# SLSA build provenance (public-good Sigstore)
gh attestation verify oci://ghcr.io/tundrawork/prohibitorum:<tag> --owner TundraWork
gh attestation verify <downloaded-archive>.tar.gz --owner TundraWork

# cosign keyless signature
cosign verify ghcr.io/tundrawork/prohibitorum:<tag> \
  --certificate-identity-regexp 'https://github.com/TundraWork/Prohibitorum/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Out of scope

Deferred Low-severity items — documented so they can be enabled in a one-line
follow-up:

- **Release concurrency guard** — `concurrency: { group: release-${{ github.ref }}, cancel-in-progress: false }`.
- **`persist-credentials: false`** on the release checkout (GoReleaser uses
  `GITHUB_TOKEN` from env, not the git credential).
- **cosign v3 `--bundle`** — replace the separate
  `--output-signature`/`--output-certificate` outputs with a single
  `.sigstore.json` bundle in the `signs` block.

Also out of scope: macOS/Windows release binaries; replacing ko with a
hand-written Dockerfile; changing the base image or SBOM format.

## Verification plan & primary risk

- Run `goreleaser release --snapshot --clean` **locally** (GoReleaser is
  mise-pinned, so versions match CI) to confirm the full build succeeds.
- **Primary risk / unknown:** does `docker_digest` capture **ko**-built image
  manifests in `dist/digests.txt`? The feature is documented generically for
  "all images and manifests published," but the docs don't explicitly confirm ko
  coverage. Settle it empirically from the snapshot run by inspecting
  `dist/digests.txt` and `dist/artifacts.json`.
  - **Fallback if ko images are absent:** derive digests from
    `dist/artifacts.json` (jq over `Docker Manifest` entries →
    `image@sha256:...`) into a checksums-format file, and point the second
    `actions/attest` step at that instead.
- After merge, exercise the pre-tag gate on a PR before ever pushing a tag.

## Files touched

- `.github/workflows/release.yml` — SHA pins, job-scoped perms, harden-runner,
  attest steps.
- `.github/workflows/ci.yml` — SHA pins, `release-check` job, path-filtered
  snapshot job.
- `.goreleaser.yaml` — add `docker_digest`.
- `mise.toml` — pin `actionlint` + `zizmor`; add `ci:release-check`,
  `ci:lint-actions`, `ci:release-snapshot` tasks.
- `TOOLING.md` / `README.md` — document the pre-tag gate, provenance, verify
  commands, and the deferred Low-severity items.
