//go:build linux

package processguard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCgroupContainsSetsidAndAppliesLimits(t *testing.T) {
	opts := testOptions()
	if opts.CgroupRoot == "" {
		t.Skip("set CSAI_TEST_CGROUP_ROOT to a delegated cgroup v2 root")
	}
	opts.Mode = "required"
	opts.CPUQuotaMicros = 50000
	id := testID()
	g, err := NewWithOptions(id, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestGroup(t, g)
	file := filepath.Join(t.TempDir(), "escaped")
	cmd, err := startTestCommand(g, fmt.Sprintf("setsid sh -c 'echo $$ > %s; exec sleep 300' </dev/null >/dev/null 2>&1 &", file))
	if err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	pid := readPID(t, file)
	<-reaped // The launching shell is gone; the cgroup must still own setsid descendants.
	root := filepath.Join(opts.CgroupRoot, "task-"+id)
	for name, want := range map[string]string{"pids.max": "64", "memory.max": "268435456", "cpu.max": "50000 100000"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || strings.TrimSpace(string(data)) != want {
			t.Fatalf("%s=%s err=%v", name, data, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	<-reaped
	waitGone(t, pid)
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("cgroup retained after cleanup: %v", err)
	}
}

func TestCgroupStartupDelegationAndRecovery(t *testing.T) {
	opts := testOptions()
	if opts.CgroupRoot == "" {
		t.Skip("requires delegated cgroup fixture")
	}
	before, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	original := ""
	for _, line := range strings.Split(string(before), "\n") {
		if strings.HasPrefix(line, "0::") {
			original = filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(line, "0::"))
		}
	}
	root := filepath.Join(opts.CgroupRoot, "startup-fixture")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "cgroup.procs"), []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.WriteFile(filepath.Join(original, "cgroup.procs"), []byte(fmt.Sprint(os.Getpid())), 0600)
		if rootLock != nil {
			_ = rootLock.Close()
			rootLock = nil
		}
		_ = removeCgroupTree(root)
	}()
	stale := filepath.Join(root, "task-"+testID())
	if err = os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(stale)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	cmd := exec.Command("sh", "-c", "exec sleep 300")
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(dir.Fd()), Setsid: true}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	opts.CgroupRoot = root
	opts.Mode = "required"
	if err = configurePlatform(&opts); err != nil {
		_ = cmd.Process.Kill()
		<-reaped
		t.Fatal(err)
	}
	<-reaped
	waitGone(t, cmd.Process.Pid)
	if _, err = os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale task cgroup was not removed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil || !strings.Contains(string(data), "memory") {
		t.Fatalf("delegation not enabled: %s %v", data, err)
	}
}
