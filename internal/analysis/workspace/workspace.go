// Package workspace materializes a PR's source tree into a temp directory
// so linters can scan it as a real filesystem. Tarballs come from the VCS
// (GitHub & GitLab both expose archive endpoints); we extract safely with
// path-traversal guards and flatten the single-root wrapper directory both
// providers wrap their archives in.
package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RepoArchiver is the optional VCS capability that returns a gzipped tar
// stream of the repo at a given ref. github.Adapter and gitlab.Adapter both
// implement it; the orchestrator type-asserts.
type RepoArchiver interface {
	FetchArchive(ctx context.Context, repo, ref string) (io.ReadCloser, error)
}

// Workspace points at an extracted source tree on disk. Always defer Close().
type Workspace struct {
	Dir string
}

// Close removes the workspace's temp directory.
func (w *Workspace) Close() error {
	if w == nil || w.Dir == "" {
		return nil
	}
	return os.RemoveAll(w.Dir)
}

// Open downloads the archive for repo@ref via archiver, extracts it to a new
// temp directory, and flattens the single-root wrapper that GitHub/GitLab
// archives are wrapped in.
func Open(ctx context.Context, archiver RepoArchiver, repo, ref string) (*Workspace, error) {
	src, err := archiver.FetchArchive(ctx, repo, ref)
	if err != nil {
		return nil, fmt.Errorf("fetch archive %s@%s: %w", repo, ref, err)
	}
	defer func() { _ = src.Close() }()

	dir, err := os.MkdirTemp("", "cadoo-ws-*")
	if err != nil {
		return nil, fmt.Errorf("mkdtemp: %w", err)
	}
	if err := extractTarGz(src, dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("extract %s@%s: %w", repo, ref, err)
	}
	if err := flattenSingleRoot(dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("flatten %s@%s: %w", repo, ref, err)
	}
	return &Workspace{Dir: dir}, nil
}

// extractTarGz unpacks a gzipped tar stream into dest. Refuses entries whose
// resolved path escapes dest (Zip-Slip / tar-traversal guard) and skips
// non-regular, non-dir entries (symlinks, devices) for safety.
func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, h.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) {
			continue // path-traversal attempt — skip
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// flattenSingleRoot moves contents of dir/<single-subdir>/* up to dir/* and
// removes the wrapper. Both GitHub and GitLab tarballs wrap content in a
// single top-level directory like "owner-repo-abcdef/".
func flattenSingleRoot(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil
	}
	sub := filepath.Join(dir, entries[0].Name())
	inner, err := os.ReadDir(sub)
	if err != nil {
		return err
	}
	for _, e := range inner {
		from := filepath.Join(sub, e.Name())
		to := filepath.Join(dir, e.Name())
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return os.Remove(sub)
}
