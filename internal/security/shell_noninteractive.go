package security

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ShellNoOutputTimeoutMessage A hint shown when there is no new stdout/stderr for a long time (soft failure, visible to the model).
func ShellNoOutputTimeoutMessage(idleSec int) string {
	return fmt.Sprintf(`Command terminated: no new output for over %d seconds, likely waiting for interactive input or already hung.

For long silent tasks please use a trailing & Run in the background, or increase agent.shell_no_output_timeout_seconds (-1 = disable this check).

Command terminated: no new output for %d seconds (possible interactive wait or hung process).`, idleSec, idleSec)
}

// ShellInactivityWatch Signal expired when there is no new output within noOutputSec; each Bump resets the timer.
// Unlike cancelling the timer permanently once the first packet arrives, this covers cases like sudo printing a Password prompt and then continuing to hang.
type ShellInactivityWatch struct {
	Sec     int
	mu      sync.Mutex
	timer   *time.Timer
	Expired chan struct{}
}

func NewShellInactivityWatch(noOutputSec int) *ShellInactivityWatch {
	sec := ResolveShellNoOutputTimeoutSeconds(noOutputSec)
	if sec <= 0 {
		return nil
	}
	w := &ShellInactivityWatch{
		Sec:     sec,
		Expired: make(chan struct{}, 1),
	}
	w.Bump()
	return w
}

func (w *ShellInactivityWatch) Bump() {
	if w == nil || w.Sec <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(time.Duration(w.Sec)*time.Second, func() {
		select {
		case w.Expired <- struct{}{}:
		default:
		}
	})
}

func (w *ShellInactivityWatch) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

// ResolveShellNoOutputTimeoutSeconds: 0=Default 300 (5 minutes); -1 = disabled; >0 = custom.
func ResolveShellNoOutputTimeoutSeconds(sec int) int {
	if sec < 0 {
		return 0
	}
	if sec == 0 {
		return 300
	}
	return sec
}

// PrependNonInteractiveShellExports Inject a generic non-interactive environment for sh -c (pager etc.), without maintaining a command blacklist.
func PrependNonInteractiveShellExports(shellCommand string) string {
	if strings.TrimSpace(shellCommand) == "" {
		return shellCommand
	}
	upper := strings.ToUpper(shellCommand)
	var pairs []string
	add := func(key, val string) {
		if strings.Contains(upper, strings.ToUpper(key)) {
			return
		}
		pairs = append(pairs, key+"="+val)
	}
	add("GIT_PAGER", "cat")
	add("PAGER", "cat")
	add("SYSTEMD_PAGER", "cat")
	add("DEBIAN_FRONTEND", "noninteractive")
	if len(pairs) == 0 {
		return shellCommand
	}
	return "export " + strings.Join(pairs, " ") + "\n" + shellCommand
}

// PrependNonInteractiveStdinRedirect Close stdin for sh -c (equivalent to attachNonInteractiveStdin),
// Make programs that read from stdin such as read/input()/sudo -S fail fast instead of hanging. Do not re-inject when </dev/null is already present.
func PrependNonInteractiveStdinRedirect(shellCommand string) string {
	if strings.TrimSpace(shellCommand) == "" {
		return shellCommand
	}
	lower := strings.ToLower(shellCommand)
	if strings.Contains(lower, "</dev/null") || strings.Contains(lower, "0</dev/null") {
		return shellCommand
	}
	return "exec </dev/null\n" + shellCommand
}

// PrepareNonInteractiveShellCommand Compose the non-interactive wrapper: stdin closed + pager and other environment variables (zero list).
func PrepareNonInteractiveShellCommand(shellCommand string) string {
	return PrependNonInteractiveStdinRedirect(PrependNonInteractiveShellExports(shellCommand))
}

// ApplyNonInteractivePagerEnv Fill in environment variables for exec.Cmd consistent with PrependNonInteractiveShellExports.
func ApplyNonInteractivePagerEnv(cmdEnv []string) []string {
	if cmdEnv == nil {
		cmdEnv = []string{}
	}
	has := func(k string) bool {
		prefix := k + "="
		for _, e := range cmdEnv {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}
	if !has("GIT_PAGER") {
		cmdEnv = append(cmdEnv, "GIT_PAGER=cat")
	}
	if !has("PAGER") {
		cmdEnv = append(cmdEnv, "PAGER=cat")
	}
	if !has("SYSTEMD_PAGER") {
		cmdEnv = append(cmdEnv, "SYSTEMD_PAGER=cat")
	}
	if !has("DEBIAN_FRONTEND") {
		cmdEnv = append(cmdEnv, "DEBIAN_FRONTEND=noninteractive")
	}
	return cmdEnv
}

// attachNonInteractiveStdin Close interactive stdin so that some commands fail fast instead of waiting for input.
func attachNonInteractiveStdin(cmd *exec.Cmd) {
	if cmd == nil || cmd.Stdin != nil {
		return
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		return
	}
	cmd.Stdin = f
}
