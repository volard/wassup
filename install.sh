#!/bin/sh

set -eu

repository="volard/wassup"
program="wassup"

if [ -n "${INSTALL_PATH:-}" ]; then
	install_path=$INSTALL_PATH
else
	: "${HOME:?HOME is not set; set INSTALL_PATH explicitly}"
	install_path="$HOME/.local/bin"
fi

case "$(uname -s)" in
	Linux) platform="linux" ;;
	Darwin) platform="darwin" ;;
	*)
		printf 'wassup: unsupported operating system: %s\n' "$(uname -s)" >&2
		exit 1
		;;
esac

case "$(uname -m)" in
	x86_64 | amd64) architecture="amd64" ;;
	aarch64 | arm64) architecture="arm64" ;;
	*)
		printf 'wassup: unsupported architecture: %s\n' "$(uname -m)" >&2
		exit 1
		;;
esac

for command in curl tar awk install mktemp; do
	if ! command -v "$command" >/dev/null 2>&1; then
		printf 'wassup: required command is missing: %s\n' "$command" >&2
		exit 1
	fi
done

archive="$program-$platform-$architecture.tar.gz"
release_url="https://github.com/$repository/releases/latest/download"
tmp_dir="$(mktemp -d 2>/dev/null || mktemp -d -t wassup)"
trap 'rm -rf "$tmp_dir"' 0

printf 'Downloading %s…\n' "$archive"
curl --fail --location --silent --show-error --retry 3 \
	--output "$tmp_dir/$archive" "$release_url/$archive"
curl --fail --location --silent --show-error --retry 3 \
	--output "$tmp_dir/checksums.txt" "$release_url/checksums.txt"

expected="$(awk -v archive="$archive" '$2 == archive { print $1; exit }' "$tmp_dir/checksums.txt")"
if [ -z "$expected" ]; then
	printf 'wassup: no checksum published for %s\n' "$archive" >&2
	exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$tmp_dir/$archive" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$tmp_dir/$archive" | awk '{ print $1 }')"
else
	printf 'wassup: sha256sum or shasum is required to verify the download\n' >&2
	exit 1
fi

if [ "$actual" != "$expected" ]; then
	printf 'wassup: checksum verification failed for %s\n' "$archive" >&2
	exit 1
fi

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
mkdir -p "$install_path"
install -m 0755 "$tmp_dir/$program" "$install_path/$program"

printf 'Installed %s to %s\n' "$program" "$install_path/$program"
case ":${PATH:-}:" in
	*":$install_path:"*) ;;
	*) printf 'Add %s to PATH to run %s from anywhere.\n' "$install_path" "$program" ;;
esac
