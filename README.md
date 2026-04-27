# bb — Bitbucket CLI

A fast, comprehensive command-line interface for Bitbucket Cloud and
Bitbucket Data Center / Server. Inspired by [`gh`](https://cli.github.com/)
and [`glab`](https://gitlab.com/gitlab-org/cli).

> Status: early scaffolding. Expect breaking changes.

## Features (planned)

- `bb auth` — log in with app password / HTTP access token / OAuth
- `bb repo` — clone, view, create, fork, list
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

## License

MIT
