#!/usr/bin/env bash
# Builds reproducible, checksummed release archives for every supported
# platform. See docs/release.md for the full release procedure this script
# is one step of.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DIST_DIR="dist"
MODULE="github.com/viq111/bdd/cmd/bdd"
MODULE_ARCHIVE="github.com/viq111/bdd/cmd/bddarchive"

# GOOS/GOARCH pairs to build. modernc.org/sqlite is a CGO-free, pure-Go
# SQLite driver, so CGO_ENABLED=0 cross-compiles cleanly to every target
# below with the stock Go toolchain and no C toolchain installed.
PLATFORMS=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
)

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"

# SOURCE_DATE_EPOCH (https://reproducible-builds.org/specs/source-date-epoch/):
# every archive entry's timestamp is pinned to this value instead of the
# moment the archive happened to be built, so repeat builds of the same
# commit produce byte-identical archives. Falls back to the Unix epoch
# outside a git checkout (e.g. an extracted source tarball).
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || echo 0)}"

echo "Releasing bdd $VERSION"

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

archive_bin="$(mktemp -d)/bddarchive"
trap 'rm -rf "$(dirname "$archive_bin")"' EXIT
go build -o "$archive_bin" "$MODULE_ARCHIVE"

for platform in "${PLATFORMS[@]}"; do
  read -r os arch <<<"$platform"

  bin_name="bdd"
  archive_ext="tar.gz"
  if [ "$os" = "windows" ]; then
    bin_name="bdd.exe"
    archive_ext="zip"
  fi

  build_dir="$DIST_DIR/bdd-${VERSION}-${os}-${arch}"
  mkdir -p "$build_dir"

  echo "Building ${os}/${arch}..."
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o "${build_dir}/${bin_name}" \
    "$MODULE"

  archive_name="bdd-${VERSION}-${os}-${arch}.${archive_ext}"
  # Archives are built by cmd/bddarchive rather than the system tar/zip so
  # entry order, timestamps, and (for tar.gz) gzip header metadata are
  # normalized identically on every platform this script runs on -- GNU
  # tar, bsdtar, and Info-ZIP disagree on flags and defaults for this.
  "$archive_bin" \
    -src "$build_dir" \
    -out "${DIST_DIR}/${archive_name}" \
    -format "$archive_ext" \
    -mtime "$SOURCE_DATE_EPOCH"
  rm -rf "$build_dir"
done

echo "Writing checksums..."
(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- *.tar.gz *.zip > SHA256SUMS
  else
    shasum -a 256 -- *.tar.gz *.zip > SHA256SUMS
  fi
)

echo "Release artifacts written to $DIST_DIR:"
ls -1 "$DIST_DIR"
