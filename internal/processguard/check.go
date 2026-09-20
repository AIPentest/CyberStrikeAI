package processguard

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Check exercises the real creation path, including clone3/Job inheritance,
// watchdog readiness, admission and cleanup. It does not start the HTTP server.
func Check(ctx context.Context) (backend string, err error) {
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	g, err := New(name)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, g.Close(ctx)) }()
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, exe, "-h")
	configureGuardian(cmd)
	launch, err := g.Prepare(cmd)
	if err != nil {
		return "", err
	}
	defer launch.Dispose()
	if err = cmd.Start(); err != nil {
		return "", err
	}
	if err = launch.Commit(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}
	err = cmd.Wait()
	if err != nil {
		return "", err
	}
	if err = g.Release(cmd.Process.Pid); err != nil {
		return "", err
	}
	return g.Name(), nil
}
