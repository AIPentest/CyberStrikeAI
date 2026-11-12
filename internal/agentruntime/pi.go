package agentruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const maxRecordBytes = 32 * 1024 * 1024

type Pi struct {
	NodePath   string
	WorkerPath string
}

type runRequest struct {
	Request
	LegacyProfile json.RawMessage `json:"profile"`
}

type record struct {
	Type       string      `json:"type"`
	Version    int         `json:"version,omitempty"`
	Request    *runRequest `json:"request,omitempty"`
	Event      *Event      `json:"event,omitempty"`
	Call       *ToolCall   `json:"call,omitempty"`
	Result     *Result     `json:"result,omitempty"`
	Error      string      `json:"error,omitempty"`
	SDKVersion string      `json:"sdkVersion,omitempty"`
}

// ResolveWorker supports source installs and release bundles without relying on PATH for scripts.
func ResolveWorker(configured string) (string, error) {
	if configured != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
			return "", fmt.Errorf("Pi worker does not exist: %s", path)
		}
		return path, nil
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "runtime/pi/dist/worker.js"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "runtime/pi/dist/worker.js"))
		// Package tests and developer commands may run below the repository root.
		for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				candidates = append(candidates, filepath.Join(dir, "runtime/pi/dist/worker.js"))
				break
			}
		}
	}
	for _, path := range candidates {
		root := filepath.Dir(filepath.Dir(path))
		if fingerprint, err := SourceFingerprint(root); err == nil {
			bundle := filepath.Join(root, ".builds", fingerprint)
			if _, err = os.Stat(filepath.Join(bundle, ".ready")); err == nil {
				if _, err = os.Stat(filepath.Join(bundle, "dist", "worker.js")); err == nil {
					return filepath.Join(bundle, "dist", "worker.js"), nil
				}
			}
			continue
		}
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", errors.New("Pi worker missing; run bash scripts/build-pi-runtime.sh, or configure agent_runtime.worker_path")
}

// Run owns one isolated worker and reaps it on every exit. Tool execution stays in the host.
func (p Pi) Run(ctx context.Context, req Request, execute ToolExecutor, emit func(Event), bind func(Control) func()) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.WorkingDir == "" {
		var err error
		req.WorkingDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	release, err := acquireSession(ctx, req.SessionDir)
	if err != nil {
		return nil, err
	}
	defer release()
	worker, err := ResolveWorker(p.WorkerPath)
	if err != nil {
		return nil, err
	}
	node := p.NodePath
	if node == "" {
		node = "node"
	}
	scratchParent, err := os.MkdirTemp("", "cyberstrike-pi-host-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratchParent)
	cmd := exec.Command(node, worker)
	cmd.Env = append(os.Environ(), "CYBERSTRIKE_PI_TMP_PARENT="+scratchParent)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	// Worker protocol carries failures. Never forward arbitrary stderr (which may include provider secrets).
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start Pi worker: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ioCtx, stopIO := context.WithCancel(context.Background())
	defer stopIO()
	var writeMu sync.Mutex
	write := func(v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if len(body) > maxRecordBytes {
			return errors.New("Pi request exceeds protocol record limit")
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = stdin.Write(append(body, '\n'))
		return err
	}
	incoming := make(chan record, 32)
	readDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), maxRecordBytes+1)
		for scanner.Scan() {
			var r record
			if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
				readDone <- fmt.Errorf("invalid Pi protocol JSON: %w", err)
				return
			}
			select {
			case incoming <- r:
			case <-ioCtx.Done():
				readDone <- ioCtx.Err()
				return
			}
		}
		readDone <- scanner.Err()
	}()
	defer func() {
		cancel()
		stopIO()
		stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		stdout.Close()
	}()
	readyTimeout := time.NewTimer(20 * time.Second)
	defer readyTimeout.Stop()
	select {
	case r := <-incoming:
		if r.Type != "ready" || r.Version != ProtocolVersion || r.SDKVersion != "1.5.2" {
			return nil, errors.New("Pi runtime protocol mismatch")
		}
	case err := <-readDone:
		return nil, fmt.Errorf("Pi worker exited before handshake: %v", err)
	case <-readyTimeout.C:
		return nil, errors.New("Pi worker startup timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Older protocol v1 workers require a profile object. Keep it disabled.
	run := runRequest{Request: req, LegacyProfile: json.RawMessage(`{"enabled":false}`)}
	if err = write(record{Type: "run", Version: ProtocolVersion, Request: &run}); err != nil {
		return nil, err
	}
	if bind != nil {
		unbind := bind(Control{
			Steer:  func(s string) error { return write(map[string]any{"type": "steer", "message": s}) },
			Cancel: func() error { cancel(); return write(map[string]any{"type": "cancel"}) },
		})
		if unbind != nil {
			defer unbind()
		}
	}
	type executed struct {
		call   string
		result ToolResult
		done   chan struct{}
	}
	var toolMu sync.Mutex
	toolCache := map[string]*executed{}
	toolSlots := make(chan struct{}, 32)
	ctxDone := ctx.Done()
	var cancelDeadline <-chan time.Time
	var cancelTimer *time.Timer
	defer func() {
		if cancelTimer != nil {
			cancelTimer.Stop()
		}
	}()
	for {
		select {
		case r := <-incoming:
			switch r.Type {
			case "event":
				if emit != nil && r.Event != nil {
					emit(*r.Event)
				}
			case "tool_call":
				if r.Call == nil || r.Call.ID == "" {
					return nil, errors.New("Pi tool call has no identity")
				}
				call := *r.Call
				select {
				case toolSlots <- struct{}{}:
				default:
					return nil, errors.New("Pi tool concurrency limit exceeded")
				}
				go func() {
					defer func() { <-toolSlots }()
					body, _ := json.Marshal(call)
					toolMu.Lock()
					cached := toolCache[call.ID]
					if cached == nil {
						cached = &executed{call: string(body), done: make(chan struct{})}
						toolCache[call.ID] = cached
						toolMu.Unlock()
						res := ToolResult{IsError: true, Content: "No host tool executor"}
						if execute != nil {
							var invokeErr error
							res, invokeErr = execute(runCtx, call)
							if invokeErr != nil {
								res.IsError = true
								res.Content = invokeErr.Error()
							}
						}
						cached.result = res
						close(cached.done)
					} else {
						toolMu.Unlock()
					}
					select {
					case <-cached.done:
					case <-runCtx.Done():
						return
					}
					res := cached.result
					if cached.call != string(body) {
						res = ToolResult{IsError: true, Content: "Tool call ID reused with different arguments"}
					}
					_ = write(map[string]any{"type": "tool_result", "id": call.ID, "result": res})
				}()
			case "result":
				if r.Result == nil {
					return nil, errors.New("Pi returned an empty result")
				}
				if ctx.Err() != nil {
					return r.Result, ctx.Err()
				}
				if r.Result.Error != "" {
					return r.Result, fmt.Errorf("Pi runtime error: %s", r.Result.Error)
				}
				if r.Result.Status == "canceled" {
					return r.Result, context.Canceled
				}
				return r.Result, nil
			case "fatal":
				return nil, fmt.Errorf("Pi protocol failure: %s", r.Error)
			default:
				return nil, fmt.Errorf("unexpected Pi record: %s", r.Type)
			}
		case err := <-readDone:
			// All queued protocol records precede EOF; process them before interpreting exit.
			if len(incoming) > 0 {
				readDone <- err
				continue
			}
			return nil, fmt.Errorf("Pi worker exited without a result: %v", err)
		case <-ctxDone:
			ctxDone = nil
			_ = write(map[string]any{"type": "cancel"})
			cancelTimer = time.NewTimer(2 * time.Second)
			cancelDeadline = cancelTimer.C
		case <-cancelDeadline:
			return nil, ctx.Err()
		}
	}
}
