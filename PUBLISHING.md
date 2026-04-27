# Publishing a Go CLI — reusable playbook

This is the end-to-end recipe for publishing a Go CLI so that users on
Linux, macOS and Windows can install it, and so that updates flow
through their native package managers (with a built-in `<tool> upgrade`
fallback).

It's written generically — replace the **placeholders** below before
copying it to another project:

| Placeholder | This repo's value |
|---|---|
| `<TOOL>` | `bb` |
| `<OWNER>` | `hugs7` |
| `<REPO>` | `bitbucket-cli` |
| `<DESC>` | "A comprehensive command-line interface for Bitbucket." |

---

## 1. Prerequisites (one-off)

1. **Go installed locally** (matching `go.mod`).
2. **A `cmd/<TOOL>/main.go`** that declares ldflag-injectable build vars:
   ```go
   var (
       version = "dev"
       commit  = "none"
       date    = "unknown"
   )
   ```
3. **`.goreleaser.yaml`** at the repo root (already present here — see
   the file for the layout). It builds Linux/macOS/Windows × amd64/arm64,
   creates `.tar.gz`/`.zip` archives, `.deb`/`.rpm`/`.apk` packages,
   plus Homebrew tap, Scoop bucket and Winget manifests.
4. **`.github/workflows/release.yml`** — runs GoReleaser on every `v*`
   tag push. Already wired up.
5. **GitHub repo settings** → *Actions* → *General* → *Workflow
   permissions* → **Read and write permissions**. Without this the
   default `GITHUB_TOKEN` cannot create the release.

## 2. Sister repos for package-manager publishing

GoReleaser can push manifests to other repos for you. Create them
**empty** under your account once:

| Channel | Repo to create | Notes |
|---|---|---|
| Homebrew | `<OWNER>/homebrew-tap` | Public. Name **must** start with `homebrew-`. |
| Scoop | `<OWNER>/scoop-bucket` | Public. |
| Winget | fork of `microsoft/winget-pkgs` | GoReleaser opens a PR against your fork; you then PR upstream. |

Then create a **classic Personal Access Token** (Settings → Developer
settings → Tokens (classic)) with scopes `repo` and `workflow`. Save
it as a repo secret named `TAP_GITHUB_TOKEN` and uncomment the
matching line in `.github/workflows/release.yml`. The default
`GITHUB_TOKEN` only has access to the current repo, which is why a PAT
is needed for cross-repo pushes.

If you don't want all three channels, delete the corresponding
`brews:`, `scoops:`, `winget:` block in `.goreleaser.yaml`.

## 3. Cutting a release

Tagging is **automated** via [release-please](https://github.com/googleapis/release-please).
Workflow:

1. Land commits on `main` using **Conventional Commit** messages:
   - `feat: add pipelines view` → minor bump
   - `fix: handle 404 on missing repo` → patch bump
   - `feat!: rename --token to --auth` or `BREAKING CHANGE:` in body → major bump
   - `chore:` / `docs:` / `test:` → no bump (still appear in changelog if you want)
2. The `release-please` workflow keeps an open PR titled
   *"chore(main): release X.Y.Z"* that updates `CHANGELOG.md` and
   `.release-please-manifest.json` based on those commits.
3. **Merge that PR.** release-please then creates the git tag
   `vX.Y.Z` and a GitHub Release stub, which triggers
   `release.yml` (GoReleaser) to build and upload everything.

To test locally without publishing anything:

```sh
make snapshot           # → dist/ contains every artifact
```

To force a tag without release-please (rarely needed):

```sh
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions runs GoReleaser, which:

1. Cross-compiles binaries for `linux|darwin|windows` × `amd64|arm64`.
2. Bundles each as `.tar.gz` (Unix) or `.zip` (Windows).
3. Builds `.deb` / `.rpm` / `.apk` packages.
4. Generates `checksums.txt`.
5. Creates the GitHub Release and uploads all artifacts.
6. Pushes a Homebrew formula to `<OWNER>/homebrew-tap`.
7. Pushes a Scoop manifest to `<OWNER>/scoop-bucket`.
8. Opens a winget-pkgs PR against your fork.

Watch progress under the *Actions* tab.

## 4. How users install

Document these in your README. They all use the artifacts produced
above.

```sh
# macOS / Linux via Homebrew
brew install <OWNER>/tap/<TOOL>

# Windows via Scoop
scoop bucket add <OWNER> https://github.com/<OWNER>/scoop-bucket
scoop install <TOOL>

# Windows via Winget (after winget-pkgs PR is merged)
winget install <OWNER>.<TOOL>

# Debian/Ubuntu — one-shot .deb
curl -LO https://github.com/<OWNER>/<REPO>/releases/latest/download/<TOOL>_<VERSION>_linux_amd64.deb
sudo dpkg -i <TOOL>_<VERSION>_linux_amd64.deb

# Fedora/RHEL — one-shot .rpm
sudo rpm -i https://github.com/<OWNER>/<REPO>/releases/latest/download/<TOOL>_<VERSION>_linux_amd64.rpm

# Anywhere — curl|sh
curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/scripts/install.sh | sh

# Windows PowerShell
irm https://raw.githubusercontent.com/<OWNER>/<REPO>/main/scripts/install.ps1 | iex
```

## 5. How users update

| Install method | Update command | Auto via? |
|---|---|---|
| Homebrew | `brew upgrade <TOOL>` | `brew upgrade` (all formulae) |
| Scoop | `scoop update <TOOL>` | `scoop update *` |
| Winget | `winget upgrade <TOOL>` | `winget upgrade --all` |
| `.deb` / `.rpm` (one-shot) | re-run install command | ❌ — `apt upgrade` won't see it |
| `.deb` / `.rpm` (apt/yum repo) | `sudo apt upgrade <TOOL>` | ✅ if you publish to a repo (see §6) |
| `curl \| sh` script | re-run, or `<TOOL> upgrade` | — |
| Direct binary download | `<TOOL> upgrade` | — |

Every release builds a `<TOOL> upgrade` command (see
`internal/cmd/upgrade.go`) that downloads and atomically replaces the
running binary. It detects the install method only loosely — it always
points users to their package manager in the help text but works fine
as a fallback.

## 6. (Optional) Making `apt upgrade` / `dnf upgrade` work

`apt`/`dnf` only update from configured **repositories**, not from
loose `.deb`/`.rpm` files. To make native upgrades work you need to
host a repo. Easiest options, no infra to run:

- **[Cloudsmith](https://cloudsmith.io/)** — free tier for OSS, hosts
  apt/yum/alpine repos. Add a `publishers:` block in
  `.goreleaser.yaml` pointing at Cloudsmith.
- **[Packagecloud](https://packagecloud.io/)** — same idea.
- **[Fury](https://gemfury.com/)** — minimal config.

Self-hosted alternative: build the repo with `aptly` / `reprepro` /
`createrepo` in CI and push it to GitHub Pages. More moving parts.

Until you do this, document one-shot install via `dpkg -i` /
`rpm -i` and rely on `<TOOL> upgrade` for updates.

## 7. (Optional) Code signing

- **macOS** — without an Apple Developer ID ($99/yr) the binary will
  trigger Gatekeeper. Workarounds: ship via Homebrew (users don't
  hit Gatekeeper), or document `xattr -d com.apple.quarantine`.
- **Windows** — without an EV cert SmartScreen will warn for the
  first few downloads of each new version. Winget/Scoop installs are
  unaffected.
- **All platforms** — GoReleaser can sign artifacts with
  [cosign](https://goreleaser.com/customization/sign/) for free,
  giving users `cosign verify-blob` proofs. Cheap to add later.

## 8. Versioning

Use semver tags (`vMAJOR.MINOR.PATCH`). `v0.x.y` signals "expect
breaking changes". Bump:

- **PATCH** — bug fixes only.
- **MINOR** — new features, backwards-compatible.
- **MAJOR** — breaking changes (or any change while `0.x`).

GoReleaser writes `version`/`commit`/`date` into the binary — verify
after release with `<TOOL> version`.

## 9. Release checklist

```
[ ] CHANGELOG / release notes drafted (or rely on auto changelog)
[ ] `make test` green on main
[ ] `make snapshot` produces every expected artifact under dist/
[ ] git tag vX.Y.Z && git push origin vX.Y.Z
[ ] Actions run finishes green
[ ] GitHub Release page lists all artifacts + checksums.txt
[ ] homebrew-tap / scoop-bucket commit appeared
[ ] winget-pkgs PR opened (if applicable)
[ ] `brew upgrade <TOOL>` / `scoop update <TOOL>` works on a clean box
[ ] `<TOOL> upgrade` reports the new version
```

## 10. Reusing this for another Go CLI

For your second project, copy these files and adjust:

| File | Change |
|---|---|
| `.goreleaser.yaml` | `project_name`, `main:`, `binary:`, `homepage`, `description`, brew/scoop/winget repo names |
| `.github/workflows/release.yml` | nothing, unless you renamed branches |
| `internal/cmd/upgrade.go` | `upgradeRepo` constant, `binName` if not `bb` |
| `scripts/install.sh` / `install.ps1` | `REPO`, `BIN` |
| `cmd/<TOOL>/main.go` | nothing if you keep the `version`/`commit`/`date` vars |
| `PUBLISHING.md` | placeholder table at the top |

Then create the `homebrew-tap` / `scoop-bucket` / winget fork repos
(or reuse the same `homebrew-tap` repo — taps can hold many formulae)
and push a `v0.1.0` tag.
