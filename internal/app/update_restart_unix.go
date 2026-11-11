//go:build darwin || linux

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"cyberstrike-ai/internal/update"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// updateRestart only replaces the executable this instance was started from.
// For example, go run and installs with a separate config directory must not
// start a different executable just because the updater wrote one there.
func (a *App) updateRestart(root string) func() (func(), error) {
	dir, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	path := filepath.Join(dir, update.Binary)
	self, err := os.Executable()
	if err != nil {
		return nil
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil || self != path {
		return nil
	}
	return func() (func(), error) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("不是普通可执行文件：%s", path)
		}
		if err := unix.Access(path, unix.X_OK); err != nil {
			return nil, err
		}
		return func() {
			a.Shutdown()
			// Keep the same PID, arguments, environment and working directory.
			// This works both in a terminal and under an existing supervisor.
			if err := unix.Exec(path, os.Args, os.Environ()); err != nil {
				a.logger.Error("无法启动更新后的二进制", zap.Error(err))
				_ = a.logger.Sync()
				os.Exit(1)
			}
		}, nil
	}
}
