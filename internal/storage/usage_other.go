//go:build !linux

package storage

// errPlatformUnsupported 由调用方转换为 Available=false，不视为致命错误。
var errPlatformUnsupported = &unsupportedPlatformError{}

type unsupportedPlatformError struct{}

func (e *unsupportedPlatformError) Error() string {
	return "filesystem usage query is only supported on linux"
}

func filesystemUsage(path string) (Filesystem, error) {
	return Filesystem{Path: path}, errPlatformUnsupported
}
