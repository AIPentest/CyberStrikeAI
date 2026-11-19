package processguard

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type processStat struct {
	group   int
	state   byte
	threads int
}

func readProcessStat(dir string) (processStat, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return processStat{}, false, err
	}
	// comm can contain spaces, parentheses and even newlines.
	stat := string(data)
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return processStat{}, false, nil
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) <= 17 || len(fields[0]) != 1 {
		return processStat{}, false, nil
	}
	group, groupErr := strconv.Atoi(fields[2])     // field 5: pgrp
	threads, threadErr := strconv.Atoi(fields[17]) // field 20: num_threads
	return processStat{group, fields[0][0], threads}, groupErr == nil && threadErr == nil, nil
}

func (p processStat) zombie() bool {
	// A zombie thread-group leader can still have live sibling threads.
	return p.state == 'Z' && p.threads == 1
}

func visitProcessGroup(root string, pgid int, visit func(int, string, processStat) bool) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	found := false
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		stat, ok, err := readProcessStat(dir)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue // the process exited during the scan
		}
		if err != nil || !ok {
			return false
		}
		if stat.group == pgid {
			found = true
			if !visit(pid, dir, stat) {
				return false
			}
		}
	}
	return found
}

func localProcFS() bool {
	// A procfs mounted for another PID namespace reports different IDs.
	self, err := os.Readlink("/proc/self")
	return err == nil && self == strconv.Itoa(os.Getpid())
}

func processGroupOnlyZombies(pgid int) bool {
	if !localProcFS() {
		return false
	}
	// The group leader is usually still present while the guard polls.
	stat, ok, _ := readProcessStat(filepath.Join("/proc", strconv.Itoa(pgid)))
	if ok && stat.group == pgid && !stat.zombie() {
		return false
	}
	return visitProcessGroup("/proc", pgid, func(_ int, _ string, stat processStat) bool {
		return stat.zombie()
	})
}

// ProcessGroupKillPending confirms all remaining live members are in D state
// with SIGKILL pending. An unreaped direct child must itself be blocked.
func ProcessGroupKillPending(pgid int, rootWaited bool) bool {
	return localProcFS() && processGroupKillPending("/proc", pgid, rootWaited)
}

func processGroupKillPending(root string, pgid int, rootWaited bool) bool {
	blocked, rootBlocked := false, false
	allExitedOrBlocked := visitProcessGroup(root, pgid, func(pid int, dir string, stat processStat) bool {
		if stat.zombie() {
			return true
		}
		if stat.state != 'D' || !hasPendingSIGKILL(dir) {
			return false
		}
		blocked = true
		rootBlocked = rootBlocked || pid == pgid
		return true
	})
	return allExitedOrBlocked && blocked && (rootWaited || rootBlocked)
}

func hasPendingSIGKILL(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		name, value, _ := strings.Cut(line, ":")
		if name != "SigPnd" && name != "ShdPnd" {
			continue
		}
		mask, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
		if err == nil && mask&(1<<(syscall.SIGKILL-1)) != 0 {
			return true
		}
	}
	return false
}
