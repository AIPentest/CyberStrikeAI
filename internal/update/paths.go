package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateFilePath refuses directory/file and symlink conflicts before git can
// remove an untracked directory or backups can follow a link outside the install.
func validateFilePath(root, rel string) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	path := root
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return &Error{Reason: "content_path_conflict", Message: "invalid file path: " + rel}
		}
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		leaf := i == len(parts)-1
		if (leaf && !info.Mode().IsRegular()) || (!leaf && !info.IsDir()) {
			return &Error{Reason: "content_path_conflict", Message: fmt.Sprintf("%s has a symlink or directory/file conflict; local content preserved; resolve it manually first", rel), Items: []string{rel}}
		}
	}
	return nil
}
