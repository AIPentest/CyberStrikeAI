//go:build windows

package processguard

import (
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowsJobOwnerHelper(t *testing.T) {
	if os.Getenv("CSAI_JOB_OWNER") != "1" {
		t.Skip("subprocess helper")
	}
	g, err := NewWithOptions(fmt.Sprintf("test-%d-%d", os.Getpid(), time.Now().UnixNano()), Options{Mode: "required"})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsJobPayload$")
	cmd.Env = append(os.Environ(), "CSAI_JOB_PAYLOAD=1")
	launch, err := g.Prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Dispose()
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = launch.Commit(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	select {}
}
func TestWindowsJobPayload(t *testing.T) {
	if os.Getenv("CSAI_JOB_PAYLOAD") != "1" {
		t.Skip("subprocess helper")
	}
	if err := os.WriteFile(os.Getenv("CSAI_JOB_PIDFILE"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Second)
}
func TestWindowsJobReapsAfterOwnerKilled(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pid")
	owner := exec.Command(os.Args[0], "-test.run=^TestWindowsJobOwnerHelper$")
	owner.Env = append(os.Environ(), "CSAI_JOB_OWNER=1", "CSAI_JOB_PIDFILE="+file)
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(file)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("job child did not start")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	_ = owner.Process.Kill()
	_ = owner.Wait()
	event, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived owner death: %d %v", event, err)
	}
}
func TestWindowsJobClose(t *testing.T) {
	g, err := NewWithOptions(fmt.Sprintf("test-%d-%d", os.Getpid(), time.Now().UnixNano()), Options{Mode: "required", CPUQuotaMicros: 100000})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Prepare(exec.Command("cmd.exe", "/c", "exit")); err == nil {
		t.Fatal("closed job admitted a process")
	}
}
