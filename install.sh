#!/bin/sh
set -eu

repo="volard/wassup"
install_dir=${INSTALL_DIR:-"$HOME/.local/bin"}
version=${VERSION:-latest}
token=${GH_TOKEN:-${GITHUB_TOKEN:-}}

if [ "$(uname -s)" != "Linux" ]; then
  echo "wassup installer: only Linux is supported" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "wassup installer: unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

if [ -z "$token" ] && command -v gh >/dev/null 2>&1; then
  token=$(gh auth token 2>/dev/null || true)
fi
if [ -z "$token" ]; then
  echo "wassup installer: set GH_TOKEN or authenticate with the GitHub CLI" >&2
  echo "The token needs access to the private $repo repository." >&2
  exit 1
fi

for command in curl sha256sum install mktemp; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "wassup installer: required command not found: $command" >&2
    exit 1
  fi
done

asset="wassup-linux-$arch"
if [ "$version" = "latest" ]; then
  release_url="https://github.com/$repo/releases/latest/download"
else
  release_url="https://github.com/$repo/releases/download/$version"
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

curl --fail --location --silent --show-error \
  --header "Authorization: Bearer $token" \
  --header "Accept: application/octet-stream" \
  "$release_url/$asset" --output "$tmp_dir/$asset"
curl --fail --location --silent --show-error \
  --header "Authorization: Bearer $token" \
  --header "Accept: application/octet-stream" \
  "$release_url/$asset.sha256" --output "$tmp_dir/$asset.sha256"

(
  cd "$tmp_dir"
  sha256sum --check "$asset.sha256"
)

mkdir -p "$install_dir"
install -m 0755 "$tmp_dir/$asset" "$install_dir/wassup"
printf 'Installed wassup to %s/wassup\n' "$install_dir"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to PATH to run wassup from any directory.\n' "$install_dir" ;;
esac
