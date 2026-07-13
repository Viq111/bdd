// Command bddarchive packages a release build directory into a
// deterministic tar.gz or zip archive. Byte-for-byte reproducibility across
// repeat runs (same inputs, same commit) requires normalizing everything a
// build-time filesystem walk would otherwise leak into the archive: entry
// order, modification times, permissions, and (for tar.gz) gzip header
// metadata. See scripts/release.sh, which invokes this for every platform
// archive, and docs/release.md.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr *os.File) int {
	fset := flag.NewFlagSet("bddarchive", flag.ContinueOnError)
	fset.SetOutput(stderr)
	srcDir := fset.String("src", "", "directory to archive (required)")
	out := fset.String("out", "", "archive path to write (required)")
	format := fset.String("format", "", `archive format: "tar.gz" or "zip" (required)`)
	epochStr := fset.String("mtime", "0", "Unix timestamp applied to every archive entry (default 0, i.e. 1970-01-01 UTC)")

	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *srcDir == "" || *out == "" || *format == "" {
		fmt.Fprintln(stderr, "bddarchive: -src, -out, and -format are required")
		return 2
	}
	epoch, err := strconv.ParseInt(*epochStr, 10, 64)
	if err != nil {
		fmt.Fprintf(stderr, "bddarchive: invalid -mtime %q: %v\n", *epochStr, err)
		return 2
	}

	var archiveErr error
	switch *format {
	case "tar.gz":
		archiveErr = writeTarGz(*srcDir, *out, epoch)
	case "zip":
		archiveErr = writeZip(*srcDir, *out, epoch)
	default:
		fmt.Fprintf(stderr, "bddarchive: unsupported -format %q (want tar.gz or zip)\n", *format)
		return 2
	}
	if archiveErr != nil {
		fmt.Fprintf(stderr, "bddarchive: %v\n", archiveErr)
		return 1
	}
	return 0
}

// walkSorted lists root and every entry beneath it as slash-separated paths
// relative to root's parent (so the archive root directory name is
// preserved as the first path component, matching what `tar`/`zip` produce
// for a directory argument), sorted lexically for a deterministic entry
// order regardless of the underlying filesystem's readdir order. A
// directory's path always sorts before its children because it is a strict
// string prefix of them.
func walkSorted(root string) ([]string, error) {
	base := filepath.Dir(root)
	var rel []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		rel = append(rel, filepath.ToSlash(r))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(rel)
	return rel, nil
}

func writeTarGz(srcDir, out string, epoch int64) error {
	entries, err := walkSorted(srcDir)
	if err != nil {
		return err
	}
	base := filepath.Dir(srcDir)

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	// A zero ModTime keeps the gzip header's mtime field at 0 and omits the
	// original filename, so the gzip wrapper itself doesn't leak build-time
	// or path information into the archive.
	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)

	for _, rel := range entries {
		full := filepath.Join(base, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			return err
		}

		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(full)
			if err != nil {
				return err
			}
		}

		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		normalizeHeader(hdr, epoch)
		hdr.Mode = int64(normalizedMode(info).Perm())

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			if err := copyFile(tw, full); err != nil {
				return err
			}
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// normalizeHeader strips every build-environment-dependent field from a tar
// header: timestamps, ownership, and any name/format metadata libarchive
// vs. GNU tar disagree on defaulting.
func normalizeHeader(hdr *tar.Header, epoch int64) {
	mtime := timeFromEpoch(epoch)
	hdr.ModTime = mtime
	hdr.AccessTime = mtime
	hdr.ChangeTime = mtime
	hdr.Uid = 0
	hdr.Gid = 0
	hdr.Uname = ""
	hdr.Gname = ""
	hdr.Format = tar.FormatPAX
}

// normalizedMode collapses a file's permission bits to one of two fixed
// values (executable or not), preserving only the type bits (dir/symlink)
// from the original mode. Actual permission bits vary with the build
// machine's umask, which would otherwise make archives from the same commit
// diverge across machines even with a pinned SOURCE_DATE_EPOCH.
func normalizedMode(info fs.FileInfo) fs.FileMode {
	mode := info.Mode()
	perm := fs.FileMode(0o644)
	if mode.IsDir() || mode&0o111 != 0 {
		perm = 0o755
	}
	return (mode &^ fs.ModePerm) | perm
}

func writeZip(srcDir, out string, epoch int64) error {
	entries, err := walkSorted(srcDir)
	if err != nil {
		return err
	}
	base := filepath.Dir(srcDir)
	mtime := timeFromEpoch(epoch)

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	for _, rel := range entries {
		full := filepath.Join(base, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			return err
		}

		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = rel
		if info.IsDir() {
			fh.Name += "/"
		}
		fh.Modified = mtime
		fh.SetModTime(mtime)
		fh.SetMode(normalizedMode(info))
		if info.Mode().IsRegular() {
			fh.Method = zip.Deflate
		}

		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(w, target); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(w, full); err != nil {
				return err
			}
		}
	}

	return zw.Close()
}

func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func timeFromEpoch(epoch int64) time.Time {
	return time.Unix(epoch, 0).UTC()
}
