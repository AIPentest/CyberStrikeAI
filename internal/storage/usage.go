package storage

// Filesystem 描述某个路径所在文件系统的容量。
type Filesystem struct {
	Path        string  `json:"path"`
	TotalBytes  int64   `json:"total_bytes"`
	FreeBytes   int64   `json:"free_bytes"`
	UsedBytes   int64   `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
	InodesTotal int64   `json:"inodes_total"`
	InodesFree  int64   `json:"inodes_free"`
	// Available 为 false 表示当前平台不支持查询，前端应隐藏容量卡片而不是显示 0。
	Available bool `json:"available"`
}

// FilesystemUsage 返回 path 所在文件系统的容量信息。
func FilesystemUsage(path string) (Filesystem, error) {
	return filesystemUsage(path)
}
