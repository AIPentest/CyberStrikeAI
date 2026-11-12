package vision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var allowedImageExt = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".webp": {}, ".gif": {},
	".bmp": {}, ".tif": {}, ".tiff": {},
}

// ResolveImagePath Parses and validates a readable image path (any directory is supported; the extension and regular-file checks still apply).
func ResolveImagePath(path string, cwd string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	cwdTrim := strings.TrimSpace(cwd)
	if cwdTrim == "" {
		var err error
		cwdTrim, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
	}
	cwdAbs, err := filepath.Abs(filepath.Clean(cwdTrim))
	if err != nil {
		return "", err
	}

	var candidate string
	if filepath.IsAbs(p) {
		candidate = filepath.Clean(p)
	} else {
		candidate = filepath.Clean(filepath.Join(cwdAbs, p))
	}
	resolved := normalizeAbsPath(candidate)
	if resolved == "" {
		return "", fmt.Errorf("invalid path")
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	if _, ok := allowedImageExt[ext]; !ok {
		return "", fmt.Errorf("unsupported image extension %q", ext)
	}

	st, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("not a regular file")
	}
	if st.Size() > 0 && st.Size() > 1<<30 {
		return "", fmt.Errorf("file too large on disk")
	}
	return resolved, nil
}

func normalizeAbsPath(p string) string {
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return ""
	}
	if link, err := filepath.EvalSymlinks(abs); err == nil {
		return link
	}
	return abs
}
