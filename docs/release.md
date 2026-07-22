# Release procedure

Owner: Programmer (bd bdd-0bms). Why this exists: phase 4's exit criterion is
a tagged v1 release with reproducible binaries and published API docs (plan
section 24). This document covers the reproducible-binary and packaging half
of that; API docs are `bd bdd-4m6x`.

## What a release produces

`bdd` is a single, CGO-free native binary (`modernc.org/sqlite` is pure Go),
so cross-compiling for every supported platform needs nothing beyond the
stock Go toolchain — no C toolchain, no Docker, no per-platform build host.

`./tools/build.sh --dist` (or its `--release` alias)
cross-compiles, packages, and checksums a release for every supported
platform:

| OS      | Architectures     |
|---------|-------------------|
| linux   | amd64, arm64      |
| darwin  | amd64, arm64      |
| windows | amd64, arm64      |

linux and darwin on amd64/arm64 are the required minimum (plan section 24).
windows builds and runs cleanly under `CGO_ENABLED=0` with no extra effort,
so it's included too — if a future dependency ever breaks that, drop it from
`PLATFORMS` in `scripts/release.sh` and note why here.

Each platform gets an archive (`.tar.gz` for linux/darwin, `.zip` for
windows) named `bdd-<version>-<os>-<arch>.{tar.gz,zip}`, containing a single
directory with the `bdd` (or `bdd.exe`) binary. A `SHA256SUMS` file alongside
them checksums every archive.

## Version stamping

`cmd/bdd/version.go` declares `var version = "dev"`, overridden at build time
via `-ldflags "-X main.version=<version>"`. `bdd version` (and
`bdd --version`/`-v`) print whatever was stamped in, with no workspace or
database access.

Both `./tools/build.sh` (local dev binaries) and `./tools/build.sh --dist` (release archives)
derive `<version>` the same way, so a locally built binary and a release
binary built from the same commit report the same version:

```sh
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
```

On a tagged, clean commit this resolves to the tag itself (e.g. `v1.0.0`).
Away from a tag it falls back to a short commit hash, `+dirty` if the
worktree has uncommitted changes, or the literal string `dev` outside a git
checkout entirely (e.g. an extracted source tarball).

## Reproducibility

Release builds pass `-trimpath` (strips local filesystem paths from the
binary) and `-ldflags "... -s -w"` (strips debug symbols and the DWARF
table) so that two builds of the same commit, on the same Go toolchain
version, produce byte-identical binaries regardless of which machine or
directory they were built in. This was verified manually: building the same
commit and version string twice from different working directories produces
matching SHA-256 checksums.

Byte-identical output depends on using the same Go toolchain version
(`go.mod` pins the minimum via `go 1.26.5`); building an old tag with a newer
Go release is not guaranteed to reproduce exactly, though it will still
produce a correct, working binary.

The `.tar.gz`/`.zip` archives themselves are also reproducible, not just the
binaries inside them: `scripts/release.sh` packages each platform's build
directory with `cmd/bddarchive` instead of the system `tar`/`zip`, which
normalizes everything a build-time filesystem walk would otherwise leak into
the archive (entry order, modification times, and gzip header metadata).
Every entry's timestamp is pinned to `SOURCE_DATE_EPOCH`, which defaults to
the release commit's committer date, so repeat builds of the same commit and
version produce byte-identical archives and a byte-identical `SHA256SUMS`.
This is covered by `TestReleaseArchivesAreReproducible` in `release_test.go`.

## Cutting a release

1. Confirm `main` is green: `./tools/build.sh --test` (build, vet, full test suite, and
   the short fuzz smoke run).
2. Tag the release commit with an annotated tag following semver, e.g.:

   ```sh
   git tag -a v1.0.0 -m "v1.0.0"
   git push origin v1.0.0
   ```

3. Build the release archives from the tagged commit:

   ```sh
   ./tools/build.sh --dist
   ```

   This writes `dist/bdd-v1.0.0-<os>-<arch>.{tar.gz,zip}` and
   `dist/SHA256SUMS`. `VERSION` is picked up automatically from the tag via
   `git describe`; override it explicitly if you ever need to rebuild a
   specific version string: `VERSION=v1.0.0 ./tools/build.sh --dist`.
4. Sanity-check one archive for the build platform before publishing:

   ```sh
   tar xzf dist/bdd-v1.0.0-$(go env GOOS)-$(go env GOARCH).tar.gz -C /tmp
   /tmp/bdd-v1.0.0-$(go env GOOS)-$(go env GOARCH)/bdd version   # prints v1.0.0
   ```

5. Publish the tag's GitHub release with the contents of `dist/` attached
   (archives + `SHA256SUMS`).
6. Once the tag is public, verify the module install path resolves it:

   ```sh
   go install github.com/viq111/bdd/cmd/bdd@v1.0.0
   bdd version   # prints v1.0.0
   ```

   `go install ...@latest` also works once the tag is the latest semver tag
   on the module's default branch.

## Re-running or automating this

`./tools/build.sh --dist` is safe to re-run — it removes and rebuilds `dist/` each time —
and has no side effects beyond writing to that directory, so it's suitable
to wire into a CI workflow triggered on tag push without further changes.
