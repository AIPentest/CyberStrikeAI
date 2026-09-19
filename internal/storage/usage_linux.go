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
	// 路径可能尚不存在，向上找最近的存在祖先，否则 Statfs 直接 ENOENT。
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
	// 用 Bavail 而不是 Bfree：f_bfree 含 root 保留块（通常约 5%），
	// 对非 root 进程会高估可用空间，导致「明明还有空间却写失败」。
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
	// 分母用 used+free 而非 total：与 gopsutil 一致，避免 root 保留块把使用率算低。
	if denom := used + free; denom > 0 {
		fs.UsedPercent = float64(used) / float64(denom) * 100
	}
	if fs.InodesFree < 0 {
		fs.InodesFree = 0
	}
	return fs, nil
}
