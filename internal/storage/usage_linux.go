//go:build linux

package storage

import (
	"os"
	"path/filepath"
	"syscall"
)

func filesystemUsage(path string) (Filesystem, error) {
	path = filepath.Clean(path)
	if path == "" {
		path = "."
	}
	// The path may not exist yet; it walks up to the nearest existing ancestor, otherwise Statfs would directly ENOENT.
	probe := path
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs(probe, &st); err != nil {
		return Filesystem{Path: path}, err
	}

	bsize := int64(st.Bsize)
	if bsize <= 0 {
		bsize = 512
	}
	total := int64(st.Blocks) * bsize
	// Uses Bavail rather than Bfree: f_bfree includes root-reserved blocks (usually about 5%),
	// For a non-root process it overestimates available space, leading to 'writes failing even though there is still space'".
	free := int64(st.Bavail) * bsize
	used := (int64(st.Blocks) - int64(st.Bfree)) * bsize
	if used < 0 {
		used = 0
	}

	fs := Filesystem{
		Path:        path,
		TotalBytes:  total,
		FreeBytes:   free,
		UsedBytes:   used,
		InodesTotal: int64(st.Files),
		InodesFree:  int64(st.Ffree),
		Available:   true,
	}
	// The denominator uses used+free rather than total: consistent with gopsutil, to avoid root-reserved blocks making the usage ratio look lower.
	if denom := used + free; denom > 0 {
		fs.UsedPercent = float64(used) / float64(denom) * 100
	}
	if fs.InodesFree < 0 {
		fs.InodesFree = 0
	}
	return fs, nil
}
