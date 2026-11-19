//go:build !linux

package processguard

func processGroupOnlyZombies(pgid int) bool { return false }

// ProcessGroupKillPending is supported only with Linux procfs.
func ProcessGroupKillPending(pgid int, rootWaited bool) bool { return false }
