package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Jail allows only regular files that stay inside Root after symlink resolution.
type Jail struct {
	Root string
}

// Resolve resolves a path within the jail.
func (j Jail) Resolve(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("empty path")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(j.Root, p)
	}
	p = filepath.Clean(p)

	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		parent, perr := filepath.EvalSymlinks(filepath.Dir(p))
		if perr != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		real = filepath.Join(parent, filepath.Base(p))
	}
	if err := j.inside(real); err != nil {
		return "", err
	}
	return real, nil
}

// inside checks if a path is inside the jail.
func (j Jail) inside(p string) error {
	rel, err := filepath.Rel(j.Root, p)
	if err != nil {
		return fmt.Errorf("%s outside working directory", p)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s outside working directory %s", p, j.Root)
	}
	return nil
}

// OpenMP3 opens an MP3 file within the jail.
func (j Jail) OpenMP3(p string, maxBytes int64) (string, os.FileInfo, error) {
	return j.OpenRegular(p, maxBytes, ".mp3")
}

// OpenRegular opens a regular file within the jail with specific extensions.
func (j Jail) OpenRegular(p string, maxBytes int64, exts ...string) (string, os.FileInfo, error) {
	real, err := j.Resolve(p)
	if err != nil {
		return "", nil, err
	}
	if len(exts) > 0 && !extOK(real, exts) {
		return "", nil, fmt.Errorf("%s: only %s allowed", filepath.Base(real), strings.Join(exts, ", "))
	}
	st, err := os.Lstat(real)
	if err != nil {
		return "", nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("%s: not opening symlink", real)
	}
	if !st.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%s: not a regular file", real)
	}
	if maxBytes > 0 && st.Size() > maxBytes {
		return "", nil, fmt.Errorf("%s: %d bytes, limit %d", filepath.Base(real), st.Size(), maxBytes)
	}
	return real, st, nil
}

// extOK checks if a file has one of the allowed extensions.
func extOK(path string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if ext == strings.ToLower(e) {
			return true
		}
	}
	return false
}
