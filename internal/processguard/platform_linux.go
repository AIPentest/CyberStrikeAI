//go:build linux

package processguard

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var rootLock *os.File // retained until server exit; never inherited by commands

func configurePlatform(o *Options) error {
	if o.CgroupRoot == "" {
		if o.Mode == "required" {
			return fmt.Errorf("required isolation needs security.process_isolation.cgroup_root")
		}
		return nil
	}
	if o.Mode == "process_group" {
		return fmt.Errorf("cgroup_root cannot be combined with process_group mode")
	}
	if o.CgroupRoot == "auto" {
		data, err := os.ReadFile("/proc/self/cgroup")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "0::") {
				o.CgroupRoot = filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(line, "0::"))
				break
			}
		}
	}
	root, err := validateRoot(o.CgroupRoot)
	if err != nil {
		return err
	}
	o.CgroupRoot = root
	// An exclusive host-side lock prevents one server's recovery sweep from
	// killing tasks owned by another server using the same delegated root.
	hash := sha256.Sum256([]byte(root))
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("cyberstrike-cgroup-%d-%x.lock", os.Getuid(), hash[:12]))
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return fmt.Errorf("cgroup root is already owned: %w", err)
	}
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	// cgroup v2 requires the delegated parent to have no processes before
	// domain controllers can be enabled. Move only this server, never outsiders.
	data, err := os.ReadFile(filepath.Join(root, "cgroup.procs"))
	if err != nil {
		return err
	}
	for _, pid := range strings.Fields(string(data)) {
		if pid != strconv.Itoa(os.Getpid()) {
			return fmt.Errorf("delegated root contains another process %s", pid)
		}
	}
	if len(strings.Fields(string(data))) > 0 {
		supervisor := filepath.Join(root, "supervisor")
		if err = os.Mkdir(supervisor, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		if err = os.WriteFile(filepath.Join(supervisor, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			return err
		}
	}
	if err = os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0600); err != nil {
		return fmt.Errorf("delegate cpu, memory and pids controllers: %w", err)
	}
	// Recover only our names under the exclusively owned root. No PID replay.
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && validTaskName(entry.Name()) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = killAndRemoveCgroup(ctx, filepath.Join(root, entry.Name()))
			cancel()
			if err != nil {
				return fmt.Errorf("recover %s: %w", entry.Name(), err)
			}
		}
	}
	rootLock = lock
	success = true
	return nil
}

func validateRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("cgroup root must be absolute")
	}
	root = filepath.Clean(root)
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if root != resolved || root == "/sys/fs/cgroup" || root == "/" {
		return "", fmt.Errorf("use a dedicated delegated cgroup, not the hierarchy root or a symlink")
	}
	var st unix.Statfs_t
	if err = unix.Statfs(root, &st); err != nil {
		return "", err
	}
	if st.Type != unix.CGROUP2_SUPER_MAGIC {
		return "", fmt.Errorf("%s is not cgroup v2", root)
	}
	return root, nil
}
func validTaskName(name string) bool {
	if !strings.HasPrefix(name, "task-") || len(name) != 41 {
		return false
	}
	for _, c := range name[5:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c == '-') {
			return false
		}
	}
	return true
}

type cgroupGroup struct {
	mu      sync.Mutex
	path    string
	dir     *os.File
	watcher *watchdog
	closed  bool
}

func newPlatformGroup(id string, o Options) (Group, error) {
	if o.CgroupRoot == "" {
		if o.Mode == "required" {
			return nil, fmt.Errorf("required isolation has no delegated cgroup root")
		}
		return newUnixGroup()
	}
	root, err := validateRoot(o.CgroupRoot)
	if err != nil {
		return nil, err
	}
	name := "task-" + id
	if !validTaskName(name) {
		return nil, fmt.Errorf("invalid task run ID")
	}
	path := filepath.Join(root, name)
	if err = os.Mkdir(path, 0700); err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(path)
		}
	}()
	limits := map[string]string{"pids.max": strconv.Itoa(o.MaxProcesses), "memory.max": strconv.FormatInt(o.MemoryMaxBytes, 10), "memory.oom.group": "1"}
	if o.CPUQuotaMicros > 0 {
		limits["cpu.max"] = fmt.Sprintf("%d 100000", o.CPUQuotaMicros)
	}
	for file, value := range limits {
		if err = os.WriteFile(filepath.Join(path, file), []byte(value), 0600); err != nil {
			return nil, fmt.Errorf("set %s: %w", file, err)
		}
	}
	if _, err = os.Stat(filepath.Join(path, "cgroup.kill")); err != nil {
		return nil, fmt.Errorf("cgroup.kill requires Linux 5.14+: %w", err)
	}
	dir, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	g := &cgroupGroup{path: path, dir: dir}
	w, err := startWatchdog(watchRequest{Name: "cgroup", Path: path}, func() {
		// A guardian crash is also fail-closed while the owner is still alive.
		_ = os.WriteFile(filepath.Join(path, "cgroup.kill"), []byte("1"), 0600)
	})
	if err != nil {
		dir.Close()
		return nil, err
	}
	g.watcher = w
	success = true
	return g, nil
}
func (g *cgroupGroup) Name() string { return "cgroup_v2" }
func (g *cgroupGroup) Prepare(cmd *exec.Cmd) (*Launch, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, fmt.Errorf("cgroup is closed")
	}
	if _, err := g.watcher.send(watchRequest{Op: "ping"}); err != nil {
		return nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// clone3(CLONE_INTO_CGROUP), not a racy write of a newly started PID.
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(g.dir.Fd())
	return &Launch{Commit: func() error { return nil }, Dispose: func() {}}, nil
}
func (g *cgroupGroup) Release(pid int) error { return nil }
func (g *cgroupGroup) Close(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.dir == nil {
		return nil
	}
	if err := killAndRemoveCgroup(ctx, g.path); err != nil {
		return err
	}
	watchErr := g.watcher.close()
	err := errors.Join(watchErr, g.dir.Close())
	g.dir = nil
	return err
}
func killAndRemoveCgroup(ctx context.Context, path string) error {
	if err := os.WriteFile(filepath.Join(path, "cgroup.kill"), []byte("1"), 0600); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for {
		data, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "populated 0") {
			return removeCgroupTree(path)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func removeCgroupTree(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			if err = removeCgroupTree(filepath.Join(path, e.Name())); err != nil {
				return err
			}
		}
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func guardianMain(dec *json.Decoder, enc *json.Encoder) error {
	var req watchRequest
	if err := dec.Decode(&req); err != nil {
		return err
	}
	if req.Name == "process_group" {
		return groupGuardian(dec, enc)
	}
	if req.Name != "cgroup" || !validTaskName(filepath.Base(req.Path)) {
		return fmt.Errorf("invalid cgroup guardian")
	}
	if _, err := validateRoot(req.Path); err != nil {
		return err
	}
	return serveGuardian(dec, enc, func(r watchRequest) error {
		if r.Op != "ping" {
			return fmt.Errorf("unknown command")
		}
		return nil
	}, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return killAndRemoveCgroup(ctx, req.Path)
	})
}
