//go:build windows

package processguard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configurePlatform(o *Options) error {
	if o.CgroupRoot != "" {
		return fmt.Errorf("cgroups are Linux-only")
	}
	if o.Mode == "process_group" {
		return fmt.Errorf("Windows tasks require Job Object containment")
	}
	return nil
}
func configureGuardian(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func gatedChildMain(args []string) error {
	return fmt.Errorf("Unix launch gates are unavailable on Windows")
}

type jobGroup struct {
	mu      sync.Mutex
	job     windows.Handle
	parent  windows.Handle
	watcher *watchdog
	closed  bool
	broken  bool
}

func newPlatformGroup(id string, o Options) (Group, error) {
	if err := configurePlatform(&o); err != nil {
		return nil, err
	}
	name := "Local\\CyberStrikeAI-" + id
	g := &jobGroup{}
	w, err := startWatchdog(watchRequest{Name: name, Options: o}, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.broken = true
		if !g.closed && g.job != 0 {
			_ = windows.TerminateJobObject(g.job, 1)
		}
	})
	if err != nil {
		return nil, err
	}
	fail := func(err error) (Group, error) { w.close(); return nil, err }
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return fail(err)
	}
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")
	h, _, callErr := proc.Call(0x0004|0x0008, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return fail(callErr)
	}
	parent, err := windows.OpenProcess(windows.PROCESS_CREATE_PROCESS|windows.PROCESS_DUP_HANDLE, false, uint32(w.cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(windows.Handle(h))
		return fail(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.broken {
		windows.CloseHandle(parent)
		windows.CloseHandle(windows.Handle(h))
		return fail(fmt.Errorf("job guardian exited during setup"))
	}
	g.job = windows.Handle(h)
	g.parent = parent
	g.watcher = w
	return g, nil
}
func (g *jobGroup) Name() string { return "windows_job" }
func (g *jobGroup) Prepare(cmd *exec.Cmd) (*Launch, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.broken {
		return nil, fmt.Errorf("job is closed")
	}
	if _, err := g.watcher.send(watchRequest{Op: "ping"}); err != nil {
		return nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Windows inherits the job at CreateProcess time from this parent. The
	// guardian joined the job BEFORE acknowledging readiness, closing the
	// Start-then-Assign race and its suspended-process crash window.
	cmd.SysProcAttr.ParentProcess = syscall.Handle(g.parent)
	return &Launch{Commit: func() error { return nil }, Dispose: func() {}}, nil
}
func (g *jobGroup) Release(pid int) error { return nil }
func (g *jobGroup) Close(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	if err := windows.TerminateJobObject(g.job, 1); err != nil {
		return err
	}
	type accounting struct {
		TotalUser, TotalKernel, PeriodUser, PeriodKernel        int64
		PageFaults, TotalProcesses, ActiveProcesses, Terminated uint32
	}
	for {
		var info accounting
		if err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return err
		}
		if info.ActiveProcesses == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	g.closed = true
	return errors.Join(g.watcher.close(), windows.CloseHandle(g.parent), windows.CloseHandle(g.job))
}
func guardianMain(dec *json.Decoder, enc *json.Encoder) error {
	var req watchRequest
	if err := dec.Decode(&req); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(req.Name)
	if err != nil {
		return err
	}
	job, err := windows.CreateJobObject(nil, name)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = uint32(req.Options.MaxProcesses + 1)
	limits.JobMemoryLimit = uintptr(req.Options.MemoryMaxBytes)
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	if req.Options.CPUQuotaMicros > 0 {
		rate := req.Options.CPUQuotaMicros / 10 / int64(runtime.NumCPU())
		if rate < 1 {
			rate = 1
		}
		if rate > 10000 {
			rate = 10000
		}
		cpu := struct{ Flags, Rate uint32 }{Flags: 1 | 4, Rate: uint32(rate)}
		if _, err = windows.SetInformationJobObject(job, windows.JobObjectCpuRateControlInformation, uintptr(unsafe.Pointer(&cpu)), uint32(unsafe.Sizeof(cpu))); err != nil {
			return err
		}
	}
	if err = windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return err
	}
	return serveGuardian(dec, enc, func(req watchRequest) error {
		if req.Op != "ping" {
			return fmt.Errorf("unknown guardian command")
		}
		return nil
	}, func() error { return windows.TerminateJobObject(job, uint32(os.Getpid())) })
}
