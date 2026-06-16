package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxArchiveBytes caps a downloaded marketplace/plugin tarball. A bundle is
// skills + small configs + scripts — large, but not unbounded. 64 MiB is
// generous while still refusing a hostile multi-gigabyte archive.
const maxArchiveBytes = 64 << 20

// maxFileBytes caps any single extracted file (a tar bomb guard).
const maxFileBytes = 8 << 20

// TreeFetcher fetches a repo ref as a tarball and extracts it under destDir,
// returning the path to the extracted repo root. Injectable for tests.
type TreeFetcher func(ctx context.Context, owner, repo, ref, destDir string) (root string, err error)

// DefaultTreeFetcher downloads github.com/{owner}/{repo} at ref via
// codeload.github.com (one HTTPS request, no git binary) and extracts it.
func DefaultTreeFetcher(ctx context.Context, owner, repo, ref, destDir string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, ref)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "eigen/plugin-install")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("repo %s/%s@%s not found (HTTP 404)", owner, repo, ref)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return extractTarGz(io.LimitReader(resp.Body, maxArchiveBytes+1), destDir)
}

// extractTarGz extracts a gzipped tar stream under destDir. GitHub tarballs nest
// everything under a single top dir ("<repo>-<ref>/"); the returned root is that
// dir. Path traversal (".." / absolute) entries are rejected.
func extractTarGz(r io.Reader, destDir string) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	topDir := ""
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar: %w", err)
		}
		// Clean + reject traversal/absolute paths.
		name := filepath.Clean(h.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			return "", fmt.Errorf("unsafe tar entry %q", h.Name)
		}
		dest := filepath.Join(destDir, name)
		// Defense in depth: the joined path must stay within destDir.
		if !withinDir(destDir, dest) {
			return "", fmt.Errorf("tar entry escapes dest: %q", h.Name)
		}
		if topDir == "" {
			// First path component is the repo's nested top dir.
			topDir = filepath.Join(destDir, firstComponent(name))
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if h.Size > maxFileBytes {
				return "", fmt.Errorf("file %q too large (%d bytes)", h.Name, h.Size)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return "", err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return "", err
			}
			n, err := io.Copy(f, io.LimitReader(tr, maxFileBytes+1))
			f.Close()
			if err != nil {
				return "", err
			}
			total += n
			if total > maxArchiveBytes {
				return "", fmt.Errorf("archive exceeds %d bytes", maxArchiveBytes)
			}
		default:
			// Skip symlinks, devices, etc. — a plugin bundle is plain files.
		}
	}
	if topDir == "" {
		return "", fmt.Errorf("empty archive")
	}
	return topDir, nil
}

// safeJoinUnder joins a manifest/catalog path under root after rejecting paths
// that are absolute or climb upward. Use this for any untrusted plugin path
// before reading/copying from a fetched repository tree.
func safeJoinUnder(root, rel, what string) (string, error) {
	rel = strings.TrimSpace(rel)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if rel == "" || rel == "." {
		return root, nil
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe %s path %q", what, rel)
	}
	joined := filepath.Join(root, clean)
	if !withinDir(root, joined) {
		return "", fmt.Errorf("%s path escapes root: %q", what, rel)
	}
	if err := ensureResolvedUnder(root, joined, what, rel); err != nil {
		return "", err
	}
	return joined, nil
}

func ensureResolvedUnder(root, path, what, rel string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve %s root: %w", what, err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // optional component paths may be absent; reads will handle it.
		}
		return fmt.Errorf("resolve %s path %q: %w", what, rel, err)
	}
	if !withinDir(resolvedRoot, resolvedPath) {
		return fmt.Errorf("%s path resolves outside root: %q", what, rel)
	}
	return nil
}

// withinDir reports whether path is inside (or equal to) dir after cleaning.
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// firstComponent returns the first path element of a cleaned relative path.
func firstComponent(p string) string {
	if i := strings.IndexByte(p, filepath.Separator); i >= 0 {
		return p[:i]
	}
	return p
}
