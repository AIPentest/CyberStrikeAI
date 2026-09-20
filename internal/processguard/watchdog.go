package processguard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const guardianArg = "--cyberstrike-internal-process-guardian"
const childArg = "--cyberstrike-internal-process-child"

type watchRequest struct {
	Op      string
	PID     int
	Path    string
	Name    string
	Options Options
}
type watchReply struct {
	PID   int
	Error string
}

type watchdog struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	input   *os.File
	output  *os.File
	encoder *json.Encoder
	decoder *json.Decoder
	done    chan struct{}
	failed  error
}

// The re-exec modes run before application configuration, listeners or MCP
// initialization. Stdin is a private pipe; no network control port is opened.
func init() {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case guardianArg:
		err := guardianMain(json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout))
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(watchReply{Error: err.Error()})
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case childArg:
		if err := gatedChildMain(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func startWatchdog(req watchRequest, onExit func()) (*watchdog, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, guardianArg)
	configureGuardian(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	// No inherited stderr pipe that could keep a caller's output reader alive.
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	w := &watchdog{cmd: cmd, input: in.(*os.File), output: out.(*os.File), done: make(chan struct{})}
	w.encoder = json.NewEncoder(w.input)
	w.decoder = json.NewDecoder(w.output)
	// The guardian only exits after EOF or failure; exit invalidates all RPCs.
	go func() {
		_ = cmd.Wait()
		close(w.done)
		if onExit != nil {
			onExit()
		}
	}()
	req.Op = "init"
	if _, err = w.send(req); err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

func (w *watchdog) send(req watchRequest) (watchReply, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed != nil {
		return watchReply{}, w.failed
	}
	type response struct {
		reply watchReply
		err   error
	}
	result := make(chan response, 1)
	// Pipe deadlines are not supported by every Windows pipe implementation.
	// On timeout kill the helper and close both ends to release this goroutine.
	go func() {
		if err := w.encoder.Encode(req); err != nil {
			result <- response{err: err}
			return
		}
		var reply watchReply
		err := w.decoder.Decode(&reply)
		if err == nil && reply.Error != "" {
			err = fmt.Errorf("process guardian: %s", reply.Error)
		}
		result <- response{reply, err}
	}()
	select {
	case r := <-result:
		w.failed = r.err
		return r.reply, r.err
	case <-time.After(3 * time.Second):
		_ = w.cmd.Process.Kill()
		_ = w.input.Close()
		_ = w.output.Close()
		w.failed = fmt.Errorf("process guardian acknowledgement timed out")
		return watchReply{}, w.failed
	}
}

func (w *watchdog) close() error {
	w.mu.Lock()
	_ = w.input.Close()
	w.mu.Unlock()
	select {
	case <-w.done:
	case <-time.After(3 * time.Second):
		_ = w.cmd.Process.Kill()
		select {
		case <-w.done:
		case <-time.After(3 * time.Second):
			return fmt.Errorf("process guardian did not exit")
		}
	}
	_ = w.output.Close()
	return nil
}

func serveGuardian(dec *json.Decoder, enc *json.Encoder, apply func(watchRequest) error, cleanup func() error) error {
	defer cleanup()
	if err := enc.Encode(watchReply{PID: os.Getpid()}); err != nil {
		return err
	}
	for {
		var req watchRequest
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		err := apply(req)
		reply := watchReply{PID: os.Getpid()}
		if err != nil {
			reply.Error = err.Error()
		}
		if err := enc.Encode(reply); err != nil {
			return err
		}
	}
}
