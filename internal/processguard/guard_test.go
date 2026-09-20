//go:build !windows

package processguard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testID() string {
	return fmt.Sprintf("%08x-1111-4111-8111-%012x", os.Getpid(), uint64(time.Now().UnixNano())&0xffffffffffff)
}
func testOptions() Options {
	return Options{CgroupRoot: os.Getenv("CSAI_TEST_CGROUP_ROOT"), MaxProcesses: 64, MemoryMaxBytes: 256 << 20}
}
func closeTestGroup(t *testing.T, g Group) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := g.Close(ctx); err != nil {
		t.Error(err)
	}
}
func startTestCommand(g Group, command string) (*exec.Cmd, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	launch, err := g.Prepare(cmd)
	if err != nil {
		return nil, err
	}
	defer launch.Dispose()
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	if err = launch.Commit(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	return cmd, nil
}
func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no PID written to %s", path)
	return 0
}
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("PID %d survived cleanup", pid)
}
func TestGuardianReapsAfterOwnerSIGKILL(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	owner := exec.Command(os.Args[0], "-test.run=^TestGuardianOwnerHelper$")
	owner.Env = append(os.Environ(), "CSAI_GUARD_TEST_OWNER=1", "CSAI_GUARD_TEST_PID="+pidPath)
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	pid := readPID(t, pidPath)
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	waitGone(t, pid)
}
func TestGuardianOwnerHelper(t *testing.T) {
	if os.Getenv("CSAI_GUARD_TEST_OWNER") != "1" {
		t.Skip("subprocess helper")
	}
	g, err := NewWithOptions(testID(), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("echo $$ > %q; exec sleep 300", os.Getenv("CSAI_GUARD_TEST_PID"))
	cmd, err := startTestCommand(g, command)
	if err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	select {}
}
func TestGroupCloseAndAdmission(t *testing.T) {
	g, err := NewWithOptions(testID(), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestGroup(t, g)
	cmd, err := startTestCommand(g, "exec sleep 300")
	if err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	closeTestGroup(t, g)
	<-reaped
	waitGone(t, cmd.Process.Pid)
	if _, err = g.Prepare(exec.Command("sh", "-c", "true")); err == nil {
		t.Fatal("closed containment accepted a command")
	}
}
func TestLaunchGateOwnerDisappearsBeforeCommit(t *testing.T) {
	if runtime.GOOS == "linux" && testOptions().CgroupRoot != "" {
		t.Skip("cgroup assignment is atomic without a gate")
	}
	g, err := NewWithOptions(testID(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestGroup(t, g)
	file := filepath.Join(t.TempDir(), "should-not-exist")
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo escaped > %q", file))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	launch, err := g.Prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	launch.Dispose() // simulate owner crashing before watchdog registration
	_ = cmd.Wait()
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("unregistered child executed user code")
	}
}
func TestRequiredIsolationFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has Job Objects")
	}
	if _, err := NewWithOptions(testID(), Options{Mode: "required"}); err == nil {
		t.Fatal("required isolation silently downgraded")
	}
}
