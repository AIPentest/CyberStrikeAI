package security

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ShellNoOutputTimeoutMessage 长时间无新 stdout/stderr 时的提示（软失败，模型可见）。
func ShellNoOutputTimeoutMessage(idleSec int) string {
	return fmt.Sprintf(`Command terminated: no new output for %d seconds; the tool may be waiting for interactive input or may have hung.

For long silent tasks, run them with a trailing & for background execution, or increase agent.shell_no_output_timeout_seconds (-1 disables this check).`, idleSec)
}

// ShellInactivityWatch 在 noOutputSec 内无任何新输出时向 expired 发送信号；每次 Bump 重置计时。
// 与「仅有首包输出就永久取消计时」不同，可兜住 sudo 打印 Password 提示后继续挂起等情况。
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

// ResolveShellNoOutputTimeoutSeconds：0=默认 300（5 分钟）；-1=关闭；>0=自定义。
func ResolveShellNoOutputTimeoutSeconds(sec int) int {
	if sec < 0 {
		return 0
	}
	if sec == 0 {
		return 300
	}
	return sec
}

// PrependNonInteractiveShellExports 为 sh -c 注入通用非交互环境（pager 等），不维护命令黑名单。
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

// PrependNonInteractiveStdinRedirect 为 sh -c 关闭 stdin（与 attachNonInteractiveStdin 等价），
// 使 read/input()/sudo -S 等从 stdin 读取的程序快速失败而非挂起。已含 </dev/null 时不重复注入。
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

// PrepareNonInteractiveShellCommand 组合非交互包装：stdin 关闭 + pager 等环境变量（零名单）。
func PrepareNonInteractiveShellCommand(shellCommand string) string {
	return PrependNonInteractiveStdinRedirect(PrependNonInteractiveShellExports(shellCommand))
}

// ApplyNonInteractivePagerEnv 为 exec.Cmd 补齐与 PrependNonInteractiveShellExports 一致的环境变量。
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

// attachNonInteractiveStdin 关闭交互式 stdin，使部分命令快速失败而非等待输入。
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
