// Package processguard provides OS containment and out-of-process crash cleanup.
// It is intentionally independent of the Agent/MCP packages so it can be
// cross-compiled and exercised without starting the application.
package processguard

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

type Options struct {
	Mode           string `yaml:"mode" json:"mode"` // auto, required, process_group
	CgroupRoot     string `yaml:"cgroup_root" json:"cgroup_root"`
	MaxProcesses   int    `yaml:"max_processes" json:"max_processes"`
	MemoryMaxBytes int64  `yaml:"memory_max_bytes" json:"memory_max_bytes"`
	CPUQuotaMicros int64  `yaml:"cpu_quota_micros" json:"cpu_quota_micros"` // per 100000 us
}

// Prepared commands must call Commit after Start and always call Dispose.
// Commit releases the Unix fallback launch gate only after watchdog ownership
// is acknowledged. Strong backends assign containment atomically at creation.
type Launch struct {
	Commit  func() error
	Dispose func()
}

type Group interface {
	Name() string
	Prepare(*exec.Cmd) (*Launch, error)
	Release(int) error
	Close(context.Context) error
}

var configured = struct {
	sync.RWMutex
	opts Options
}{opts: Options{Mode: "auto", MaxProcesses: 256, MemoryMaxBytes: 2 << 30}}

func normalize(o Options) (Options, error) {
	if o.Mode == "" {
		o.Mode = "auto"
	}
	if o.Mode != "auto" && o.Mode != "required" && o.Mode != "process_group" {
		return o, fmt.Errorf("invalid process isolation mode %q", o.Mode)
	}
	if o.MaxProcesses == 0 {
		o.MaxProcesses = 256
	}
	if o.MemoryMaxBytes == 0 {
		o.MemoryMaxBytes = 2 << 30
	}
	if o.MaxProcesses < 1 || o.MaxProcesses > 65535 || o.MemoryMaxBytes < 0 || o.CPUQuotaMicros < 0 {
		return o, fmt.Errorf("invalid process isolation resource limits")
	}
	return o, nil
}

// Configure validates deployment before accepting any tasks. An explicit root
// or required mode fails closed; it never silently falls back after an error.
func Configure(o Options) error {
	var err error
	o, err = normalize(o)
	if err != nil {
		return err
	}
	if err = configurePlatform(&o); err != nil {
		return err
	}
	configured.Lock()
	configured.opts = o
	configured.Unlock()
	return nil
}
func New(id string) (Group, error) {
	configured.RLock()
	o := configured.opts
	configured.RUnlock()
	return NewWithOptions(id, o)
}
func NewWithOptions(id string, o Options) (Group, error) {
	var err error
	o, err = normalize(o)
	if err != nil {
		return nil, err
	}
	return newPlatformGroup(id, o)
}
