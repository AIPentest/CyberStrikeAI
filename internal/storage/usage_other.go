//go:build !linux

package storage

// errPlatformUnsupported Converted by the caller into Available=false, and not treated as a fatal error.
var errPlatformUnsupported = &unsupportedPlatformError{}

type unsupportedPlatformError struct{}

func (e *unsupportedPlatformError) Error() string {
	return "filesystem usage query is only supported on linux"
}

func filesystemUsage(path string) (Filesystem, error) {
	return Filesystem{Path: path}, errPlatformUnsupported
}
