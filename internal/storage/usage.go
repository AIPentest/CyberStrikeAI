package storage

// Filesystem Describes the capacity of the filesystem that a path is on.
type Filesystem struct {
	Path        string  `json:"path"`
	TotalBytes  int64   `json:"total_bytes"`
	FreeBytes   int64   `json:"free_bytes"`
	UsedBytes   int64   `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
	InodesTotal int64   `json:"inodes_total"`
	InodesFree  int64   `json:"inodes_free"`
	// Available false means the current platform does not support the query, and the frontend should hide the capacity card rather than show it 0.
	Available bool `json:"available"`
}

// FilesystemUsage Returns the capacity information of the filesystem that path is on.
func FilesystemUsage(path string) (Filesystem, error) {
	return filesystemUsage(path)
}
