//go:build !windows

package processguard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type unixGroup struct {
	mu      sync.Mutex
	watcher *watchdog
	pids    map[int]struct{}
	closed  bool
}

func newUnixGroup() (*unixGroup, error) {
	g := &unixGroup{pids: make(map[int]struct{})}
	w, err := startWatchdog(watchRequest{Name: "process_group"}, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.closed = true
		for pid := range g.pids {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	if err != nil {
		return nil, err
	}
	g.watcher = w
	return g, nil
}
func (g *unixGroup) Name() string     { return "process_group_watchdog" }
func configureGuardian(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

type childSpec struct {
	Path string
	Args []string
}

func (g *unixGroup) Prepare(cmd *exec.Cmd) (*Launch, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, fmt.Errorf("process group is closed")
	}
	// A dead guardian rejects subsequent launches before user code is executed.
	if _, err := g.watcher.send(watchRequest{Op: "ping"}); err != nil {
		return nil, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	spec, _ := json.Marshal(childSpec{Path: cmd.Path, Args: cmd.Args})
	exe, err := os.Executable()
	if err != nil {
		read.Close()
		write.Close()
		return nil, err
	}
	fd := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, read)
	cmd.Path = exe
	cmd.Args = []string{exe, childArg, strconv.Itoa(fd), base64.RawStdEncoding.EncodeToString(spec)}
	return &Launch{Dispose: func() { read.Close(); write.Close() }, Commit: func() error {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.closed {
			return fmt.Errorf("process group is closed")
		}
		pid := cmd.Process.Pid
		if _, err := g.watcher.send(watchRequest{Op: "add", PID: pid}); err != nil {
			return err
		}
		g.pids[pid] = struct{}{}
		_, err := write.Write([]byte{1})
		return err
	}}, nil
}
func (g *unixGroup) Release(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.pids[pid]; !ok {
		return nil
	}
	select {
	case <-g.watcher.done:
		delete(g.pids, pid)
		return nil
	default:
	}
	if _, err := g.watcher.send(watchRequest{Op: "release", PID: pid}); err != nil {
		return err
	}
	delete(g.pids, pid)
	return nil
}
func (g *unixGroup) Close(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	for pid := range g.pids {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	for {
		for pid := range g.pids {
			if syscall.Kill(-pid, 0) == syscall.ESRCH {
				delete(g.pids, pid)
			}
		}
		if len(g.pids) == 0 {
			return g.watcher.close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func gatedChildMain(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("invalid internal launch")
	}
	fd, err := strconv.Atoi(args[0])
	if err != nil || fd < 3 {
		return fmt.Errorf("invalid launch gate")
	}
	gate := os.NewFile(uintptr(fd), "launch-gate")
	var token [1]byte
	if _, err = io.ReadFull(gate, token[:]); err != nil {
		return fmt.Errorf("owner exited before launch: %w", err)
	}
	gate.Close()
	if token[0] != 1 {
		return fmt.Errorf("invalid launch token")
	}
	b, err := base64.RawStdEncoding.DecodeString(args[1])
	if err != nil {
		return err
	}
	var spec childSpec
	if err = json.Unmarshal(b, &spec); err != nil {
		return err
	}
	return syscall.Exec(spec.Path, spec.Args, os.Environ())
}
func groupGuardian(dec *json.Decoder, enc *json.Encoder) error {
	pids := make(map[int]struct{})
	return serveGuardian(dec, enc, func(req watchRequest) error {
		switch req.Op {
		case "ping":
		case "add":
			if req.PID <= 1 {
				return fmt.Errorf("invalid PID")
			}
			pids[req.PID] = struct{}{}
		case "release":
			delete(pids, req.PID)
		default:
			return fmt.Errorf("unknown guardian command")
		}
		return nil
	}, func() error {
		for pid := range pids {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		return nil
	})
}
