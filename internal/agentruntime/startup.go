package agentruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

// EnsurePiRuntime runs in the NEW server before it starts accepting requests.
// The first upgrade from Eino is executed by the OLD updater, which only builds
// Go and knows nothing about Pi. Do not rely on that updater preparing a worker.
func EnsurePiRuntime(ctx context.Context, node, configuredWorker string) error {
	worker, err := ResolveWorker(configuredWorker)
	if err != nil && configuredWorker == "" {
		for _, candidate := range workerCandidates() {
			root := filepath.Dir(filepath.Dir(candidate))
			if _, sourceErr := SourceFingerprint(root); sourceErr != nil {
				continue
			}
			worker, err = preparePiRuntime(ctx, root, node)
			break
		}
	}
	if err != nil {
		return fmt.Errorf("prepare Pi runtime: %w", err)
	}
	if node == "" {
		node = "node"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, worker)
	// Keep stdin open until the handshake has been read. The worker exits on EOF.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Pi runtime (requires Node.js >=22.19): %w", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxRecordBytes)
	var ready record
	if !scanner.Scan() || json.Unmarshal(scanner.Bytes(), &ready) != nil ||
		ready.Type != "ready" || ready.Version != ProtocolVersion || ready.SDKVersion != "1.5.2" {
		return fmt.Errorf("Pi runtime readiness check failed; verify Node.js >=22.19 and rebuild with bash scripts/build-pi-runtime.sh")
	}
	return nil
}
