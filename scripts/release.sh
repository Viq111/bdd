#!/usr/bin/env bash
# Builds reproducible, checksummed release archives for every supported
# platform. See docs/release.md for the full release procedure this script
# is one step of.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DIST_DIR="dist"
MODULE="github.com/viq111/bdd/cmd/bdd"

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

echo "Releasing bdd $VERSION"

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

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
  (
    cd "$DIST_DIR"
    if [ "$archive_ext" = "zip" ]; then
      zip -qr "$archive_name" "bdd-${VERSION}-${os}-${arch}"
    else
      tar czf "$archive_name" "bdd-${VERSION}-${os}-${arch}"
    fi
  )
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
