package security

import (
	"context"
	"io"
	"sync"
)

type ShellExecuteRequest struct {
	Command            string
	RunInBackendGround bool
}
type ShellExecuteResponse struct {
	Output   string
	ExitCode *int
}
type shellStreamItem struct {
	response *ShellExecuteResponse
	err      error
}

type ShellStream struct {
	items  <-chan shellStreamItem
	cancel context.CancelFunc
}

func (s *ShellStream) Recv() (*ShellExecuteResponse, error) {
	item, ok := <-s.items
	if !ok {
		return nil, io.EOF
	}
	return item.response, item.err
}
func (s *ShellStream) Close() { s.cancel() }

type shellStreamWriter struct {
	ctx   context.Context
	items chan shellStreamItem
	once  sync.Once
}

func (w *shellStreamWriter) Send(response *ShellExecuteResponse, err error) bool {
	select {
	case <-w.ctx.Done():
		return true
	case w.items <- shellStreamItem{response, err}:
		return false
	}
}
func (w *shellStreamWriter) Close() { w.once.Do(func() { close(w.items) }) }
func newShellStream(ctx context.Context, cancel context.CancelFunc, size int) (*ShellStream, *shellStreamWriter) {
	items := make(chan shellStreamItem, size)
	return &ShellStream{items, cancel}, &shellStreamWriter{ctx: ctx, items: items}
}
