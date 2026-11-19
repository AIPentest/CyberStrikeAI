//go:build !windows

package processguard

import "syscall"

// ProcessGroupExists reports whether the group can still have running members.
func ProcessGroupExists(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	switch syscall.Kill(-pgid, 0) {
	case syscall.ESRCH:
		return false
	case nil:
		return !processGroupOnlyZombies(pgid)
	default:
		return true
	}
}
