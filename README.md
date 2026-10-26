# bb — Bitbucket CLI

A fast, comprehensive command-line interface for Bitbucket Cloud and
Bitbucket Data Center / Server. Inspired by [`gh`](https://cli.github.com/)
and [`glab`](https://gitlab.com/gitlab-org/cli).

> Status: early scaffolding. Expect breaking changes.

## Features (planned)

- `bb auth` — log in with app password / HTTP access token / OAuth
- `bb repo` — clone, view, create, fork, list, settings / webhooks
- `bb pr` — list, view, create, checkout, diff, review, merge
- `bb pipelines` — list, view, logs, run, cancel (Bitbucket Pipelines & Bamboo)
- `bb issue` — list, view, create, comment
- `bb browse` — open the current repo / PR / pipeline in a browser
- `bb api` — raw API passthrough (like `gh api`)
- `bb completion` — shell completions for bash / zsh / fish / powershell

Works against:

- **Bitbucket Cloud** (`bitbucket.org`, REST API 2.0)
- **Bitbucket Data Center / Server** (self-hosted, REST API 1.0)

## Install

Pick the method for your platform. See [PUBLISHING.md](PUBLISHING.md)
for how releases are built.

### Package managers (recommended)

```sh
# macOS / Linux — Homebrew
brew install hugs7/tap/bitbucket-cli

# Windows — Scoop
scoop bucket add hugs7 https://github.com/hugs7/scoop-bucket
scoop install bb

# Debian / Ubuntu — apt
curl -1sLf 'https://dl.cloudsmith.io/public/hugs7/bitbucket-cli/setup.deb.sh' | sudo -E bash
sudo apt install bitbucket-cli

# Fedora / RHEL — dnf
curl -1sLf 'https://dl.cloudsmith.io/public/hugs7/bitbucket-cli/setup.rpm.sh' | sudo -E bash
sudo dnf install bitbucket-cli

# Alpine — apk
curl -1sLf 'https://dl.cloudsmith.io/public/hugs7/bitbucket-cli/setup.alpine.sh' | sudo -E bash
sudo apk add bitbucket-cli
```

### Install scripts (no package manager required)

Useful for CI, Docker images, exotic distros, or just trying it
quickly:

```sh
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/hugs7/bitbucket-cli/main/scripts/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/hugs7/bitbucket-cli/main/scripts/install.ps1 | iex
```

### Manual install (macOS + zsh, no sudo, no Homebrew)

Drops the binary into `~/.local/bin/bb` and the zsh completion into
`~/.zsh/completions/_bb`, and adds the necessary `fpath` snippet to
`~/.zshrc` (idempotent — re-run any time to upgrade in place):

```sh
curl -fsSL https://raw.githubusercontent.com/hugs7/bitbucket-cli/main/scripts/install-mac-zsh.sh | sh
```

Make sure `~/.local/bin` is on your `$PATH`.

### From source

```sh
git clone https://github.com/hugs7/bitbucket-cli
cd bitbucket-cli
go build -o bb ./cmd/bb
./bb --help
```

## Updating

| Installed via | Update with |
|---|---|
| Homebrew | `brew upgrade bitbucket-cli` |
| Scoop | `scoop update bb` |
| apt (Cloudsmith) | `sudo apt update && sudo apt upgrade bitbucket-cli` |
| dnf (Cloudsmith) | `sudo dnf upgrade bitbucket-cli` |
| apk (Cloudsmith) | `sudo apk upgrade bitbucket-cli` |
| `curl \| sh` script / direct binary | `bb upgrade` |

`bb upgrade` checks GitHub Releases and atomically replaces the
running binary on Linux, macOS and Windows. Use `bb upgrade --check`
to peek without installing.

## Configuration

Config lives at `~/.config/bb/config.yml` (override with `BB_CONFIG`).

```yaml
default_host: bitbucket.org
hosts:
  bitbucket.org:
    type: cloud
    username: your-username
    # token stored separately in OS keyring or env BB_TOKEN
  bitbucket.mycorp.example:
    type: server
    api_base: https://bitbucket.mycorp.example/rest/api/1.0
```

Per-repo overrides go in `.bb.yml` at the repo root.

## Creating pull requests

`bb prs` → `C`, or `bb pr create`, opens the creation form. Target suggestions
prefer the closest shared ancestor with the source branch, then the newest
branch-tip commit. Unrelated branches sort alphabetically. Press **Tab** on the
empty target field to accept the first suggestion, or **↑/↓** to cycle first.

```sh
bb pr create -a                       # --auto-target: infer target from local history
bb pr create -a -y --title "My fix"   # non-interactive, repository default reviewers
bb pr create -T release/current -r alice,bob -y
bb pr create -T release/current --no-reviewers -y
```

`--auto-target` and `--target` are mutually exclusive. Git does not record a
branch's original parent: this is a history-based inference, not a guarantee.
Ranking uses local heads and the selected Bitbucket remote's tracking refs,
without fetching. Fetch that remote to refresh stale history. The graph walk is
capped at 50,000 commits and three seconds; missing history falls back to names,
and `--auto-target` fails if no shared ancestor can be found. Non-interactive
creation without either flag retains the repository default target.

Applicable repository/project default reviewers start selected after choosing
source and target. Toggle them, add usernames, or submit a directory search to
add candidates without losing selections. `--reviewer` replaces defaults;
`--no-reviewers` explicitly suppresses them. Defaults failing to load aborts
creation rather than silently omitting reviewers. Cloud accepts UUIDs/names;
directory search availability depends on the backend.

When multiple remotes exist, automatic repository detection prefers configured
Bitbucket hosts or recognisable Bitbucket remotes over a GitHub origin. The
creation TUI's optional push also uses that remote and preserves any existing
upstream. Use `-R host/PROJ/repo` to select an API repository explicitly.

## Repository settings

`bb repo settings` opens the settings TUI. Supported Server/Data Center panels
offer **e** to edit JSON, **m** for minimum approvals, and **a/d** for the panel's
add/remove or enable/disable actions. Every write requires confirmation.

```sh
bb repo settings get pull-requests -R PROJ/repo
bb repo settings update pull-requests --minimum-approvals 2 -R PROJ/repo
bb repo settings update merge-strategies --file strategies.json -R PROJ/repo
bb repo settings update default-reviewers --id 4 --file condition.json -R PROJ/repo
bb repo settings update default-branch --file branch.json -R PROJ/repo
bb repo settings update permissions-users --id alice --file permission.json -R PROJ/repo
```

Resources include `details`, `default-branch`, `permissions-users`,
`permissions-groups`, `branch-restrictions`, `access-keys`, `hooks`,
`hook-settings`, `pull-requests`, `merge-strategies`, `default-reviewers`, and
`auto-decline`. Run `bb repo settings update --help` for payloads and supported
operations. JSON updates preserve omitted fields; arrays replace existing arrays.
Use `--yes` only when the change is already authorised; `--file -` also needs it.
Hook configurations can contain secrets and require `--include-sensitive` for
CLI reads; they are excluded from overview dumps.

This is **not universal Bitbucket GUI parity**: Cloud settings are read-only
apart from webhooks; HTTP-token creation, reviewer groups, description templates,
named required-build policies and plugin-specific administration still require
their supported UI/API. Logs are read-only. Repository admins cannot edit
project-inherited reviewer conditions here. Server version and permissions can
make individual resources unavailable; errors are shown, not hidden.

## Temporary admin merge overrides

The merge dialog's **b** checkbox is off by default. CLI equivalent:

```sh
bb pr merge 123 --bypass-checks -y
bb pr recover-merge-checks -R host/PROJ/repo
```

Only use this with explicit permission to merge **and** temporarily change
repository-wide checks. It snapshots applicable reviewer minima and enabled
merge hooks, saves a private recovery journal, disables checks, merges, then
restores and verifies settings before retargeting dependent PRs. Source deletion
remains a separate opt-in. Inherited merge hooks restore project inheritance;
project settings are never changed.

Other PRs can merge during the temporary window; coordinate with administrators.
Normal errors trigger restoration. Process termination, network failure or
restoration failure can require `recover-merge-checks`, which restores only the
saved settings and never retries the merge. Journals live in the OS user config
directory under `bb/merge-recovery`; do not delete them before recovery or publish
their contents. Existing journals block another bypass.

Bypass currently supports the verified Westpac Server approver schema on
Linux/WSL/macOS. Unknown schemas, unsupported reviewer matchers and inherited
reviewer requirements are refused before changes. Cloud and native Windows
bypass are unsupported; normal merges remain available. Recovery reapplies the
saved baseline, so coordinate any concurrent admin changes first.

## Custom keybindings

Override any TUI key in the PR browser by adding a `keybindings` block to your
config. Names use the snake-case form of the internal binding (e.g. `merge`,
`approve`, `edit_target`, `diff_search_next`); values are comma-separated key
strings as accepted by [bubbles/key](https://github.com/charmbracelet/bubbles/tree/main/key)
(e.g. `y`, `shift+m`, `ctrl+x`).

```yaml
keybindings:
  approve: "y, A"
  merge: "g"
  copy_link: "Y"
  diff_search: "ctrl+f"
```

Unknown names produce a single warning toast at startup; the rest of the map is
still applied. Only single-keystroke bindings are supported today (vim-style
sequences like `g m` are not — yet).

## License

MIT
