//go:build darwin || linux

package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"cyberstrike-ai/internal/update"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

const updateSessionsEnv = "CYBERSTRIKE_UPDATE_SESSIONS_FD"
const updateActivationEnv = "CYBERSTRIKE_UPDATE_ACTIVATION"

// TakeUpdateActivation identifies the update that launched this process.
// A fresh start by a supervisor has no activation ID.
func TakeUpdateActivation() string {
	value := os.Getenv(updateActivationEnv)
	_ = os.Unsetenv(updateActivationEnv)
	pid, id, ok := strings.Cut(value, ":")
	if !ok || pid != strconv.Itoa(os.Getpid()) {
		return ""
	}
	return id
}

// TakeUpdateSessionHandoff claims the descriptor left by an in-place exec.
// Do this before startup runs any subprocesses.
func TakeUpdateSessionHandoff() *os.File {
	value := os.Getenv(updateSessionsEnv)
	if value == "" {
		return nil
	}
	_ = os.Unsetenv(updateSessionsEnv)
	pid, rawFD, ok := strings.Cut(value, ":")
	fd, err := strconv.Atoi(rawFD)
	if !ok || pid != strconv.Itoa(os.Getpid()) || err != nil || fd < 3 {
		return nil
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 0 {
		return nil
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "update-sessions")
}

func writeUpdateSessionHandoff(payload []byte) (*os.File, error) {
	f, err := os.CreateTemp("", "cyberstrike-update-sessions-*")
	if err != nil {
		return nil, err
	}
	// The private file has no path by the time it contains session data.
	if err = os.Remove(f.Name()); err == nil {
		if _, err = f.Write(payload); err == nil {
			_, err = f.Seek(0, io.SeekStart)
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// updateRestart only replaces the executable this instance was started from.
// For example, go run and installs with a separate config directory must not
// start a different executable just because the updater wrote one there.
func (a *App) updateRestart(root string) func(string) (func(), error) {
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
	return func(activationID string) (func(), error) {
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
			exec := func(payload []byte) error {
				env := append(os.Environ(), fmt.Sprintf("%s=%d:%s", updateActivationEnv, os.Getpid(), activationID))
				if len(payload) != 0 {
					f, err := writeUpdateSessionHandoff(payload)
					if err == nil {
						fd := f.Fd()
						// Prevent a concurrent subprocess from inheriting the descriptor.
						syscall.ForkLock.Lock()
						defer syscall.ForkLock.Unlock()
						defer f.Close()
						_, err = unix.FcntlInt(fd, unix.F_SETFD, 0)
						if err == nil {
							env = append(env, fmt.Sprintf("%s=%d:%d", updateSessionsEnv, os.Getpid(), fd))
							return unix.Exec(path, os.Args, env)
						}
					}
					a.logger.Warn("无法交接登录会话，继续切换版本", zap.Error(err))
				}
				return unix.Exec(path, os.Args, env)
			}
			var err error
			if a.auth != nil {
				err = a.auth.HandoffSessions(exec)
			} else {
				err = exec(nil)
			}
			if err != nil {
				a.logger.Error("无法启动更新后的二进制", zap.Error(err))
				_ = a.logger.Sync()
				os.Exit(1)
			}
		}, nil
	}
}
