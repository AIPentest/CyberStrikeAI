package processguard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWatchdogClosesWithZombieOnlyGroup(t *testing.T) {
	group, err := newUnixGroup()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestGroup(t, group) })
	cmd, err := startTestCommand(group, "exit 0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	pid := cmd.Process.Pid
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	deadline := time.Now().Add(time.Second)
	for {
		stat, ok, err := readProcessStat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ok && stat.zombie() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PID %d did not become a zombie", pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if syscall.Kill(-pid, 0) != nil {
		t.Fatal("fixture did not retain a process group")
	}
	if ProcessGroupExists(pid) {
		t.Fatal("zombie-only group counted as live")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProcessGroupKillPending(t *testing.T) {
	for _, tc := range []struct {
		name       string
		leader     byte
		member     byte
		threads    int
		signalMask string
		rootWaited bool
		want       bool
	}{
		{"blocked descendant", 'Z', 'D', 1, "00000100", true, true},
		{"blocked leader", 'D', 'Z', 1, "00000100", false, true},
		{"direct child not waited", 'Z', 'D', 1, "00000100", false, false},
		{"zombie leader has live threads", 'Z', 'D', 2, "00000100", true, false},
		{"stopped process", 'Z', 'T', 1, "00000100", true, false},
		{"sleeping process", 'Z', 'S', 1, "00000100", true, false},
		{"zombies only", 'Z', 'Z', 1, "00000100", true, false},
		{"different pending signal", 'Z', 'D', 1, "00004000", true, false},
		{"status unavailable", 'Z', 'D', 1, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeProcFixture(t, root, 100, 100, tc.leader, tc.threads, tc.signalMask)
			writeProcFixture(t, root, 101, 100, tc.member, 1, tc.signalMask)
			writeProcFixture(t, root, 200, 200, 'R', 1, "")
			if got := processGroupKillPending(root, 100, tc.rootWaited); got != tc.want {
				t.Errorf("processGroupKillPending = %v, want %v", got, tc.want)
			}
		})
	}
}

func writeProcFixture(t *testing.T, root string, pid, pgid int, state byte, threads int, signalMask string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (command ) with newline\n) %c 1 %d%s %d 0\n", pid, state, pgid, strings.Repeat(" 0", 14), threads)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	if signalMask != "" {
		status := "SigPnd:\t00000000\nShdPnd:\t" + signalMask + "\n"
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
