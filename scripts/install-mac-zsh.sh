#!/usr/bin/env sh
# Manual bb install for macOS + zsh — no sudo, no Homebrew.
#
# Drops the binary into ~/.local/bin/bb and the zsh completion into
# ~/.zsh/completions/_bb. Adds a fpath snippet to ~/.zshrc the first
# time it runs (idempotent — re-runs don't duplicate). Re-run any
# time to upgrade in place; bump V to pin a different release.
#
# Apple Silicon by default; for Intel Macs change ARCH to "amd64".

set -eu

V=0.4.0
ARCH=arm64

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

curl -fsSL -o "$T/bb.tar.gz" \
  "https://github.com/hugs7/bitbucket-cli/releases/download/v${V}/bb_${V}_darwin_${ARCH}.tar.gz"
tar xzf "$T/bb.tar.gz" -C "$T"

mkdir -p "$HOME/.local/bin" "$HOME/.zsh/completions"
install -m 755 "$T/bb" "$HOME/.local/bin/bb"
cp "$T/completions/_bb" "$HOME/.zsh/completions/_bb"

if ! grep -q 'bb completions' "$HOME/.zshrc" 2>/dev/null; then
  printf '\n# bb completions\nfpath=("$HOME/.zsh/completions" $fpath)\nautoload -Uz compinit && compinit\n' >> "$HOME/.zshrc"
fi

bb version
echo "open a new shell (or 'exec zsh') for completion to load"
