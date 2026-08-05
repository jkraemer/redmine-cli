#!/usr/bin/env bash
# Download the redmine-cli binary matching this platform from GitHub
# Releases into the directory this script lives in (next to SKILL.md),
# verifying it against the release's checksums.txt.
#
# Usage: ./bootstrap.sh [version]    (default: the latest release)
set -euo pipefail

REPO="jkraemer/redmine-cli"
DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION="${1:-}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *)
    echo "error: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac
case "$os" in
  linux | darwin) ;;
  *)
    echo "error: unsupported OS: $os (on Windows, download the zip from https://github.com/$REPO/releases)" >&2
    exit 1
    ;;
esac

if [ -z "$VERSION" ]; then
  # tag_name of the latest release, without jq so the script has no
  # dependencies beyond curl + tar + a sha256 tool.
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -1)"
fi
if [ -z "$VERSION" ]; then
  echo "error: could not determine the latest release of $REPO" >&2
  exit 1
fi

archive="redmine-cli_${VERSION}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/v$VERSION"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "downloading $archive ..."
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

# checksums.txt covers all release artifacts; verify just ours.
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $archive\$" checksums.txt | sha256sum -c -)
else
  (cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c -)
fi

tar -xzf "$tmp/$archive" -C "$tmp" redmine-cli
mv "$tmp/redmine-cli" "$DIR/redmine-cli"
chmod +x "$DIR/redmine-cli"
echo "installed: $("$DIR/redmine-cli" version)"
