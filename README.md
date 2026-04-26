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

> Not yet published. For now:

```sh
git clone https://github.com/hugo/bb
cd bb
go build -o bb ./cmd/bb
./bb --help
```

Planned distribution (via [GoReleaser](https://goreleaser.com/)):

```sh
brew install bitbucket-cli/tap/bb     # macOS / Linux
sudo apt install bb                   # Debian / Ubuntu (apt repo)
winget install bb                     # Windows
scoop install bb                      # Windows
```

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
