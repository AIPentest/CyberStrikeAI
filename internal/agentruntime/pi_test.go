package agentruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureWorker(t *testing.T, body string) Pi {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable")
	}
	path := filepath.Join(t.TempDir(), "worker.cjs")
	script := `const readline=require('node:readline');
const send=x=>process.stdout.write(JSON.stringify(x)+'\n');
send({type:'ready',version:1,sdkVersion:'1.5.2'});
const input=readline.createInterface({input:process.stdin});
` + body
	if err = os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return Pi{NodePath: node, WorkerPath: path}
}

func TestPiHostDeduplicatesToolCalls(t *testing.T) {
	p := fixtureWorker(t, `let replies=0;input.on('line',line=>{const r=JSON.parse(line);if(r.type==='run'){
const call={id:'same-id',name:'echo',arguments:{text:'hello'}};send({type:'tool_call',call});send({type:'tool_call',call});
}else if(r.type==='tool_result' && ++replies===2){send({type:'result',result:{status:'completed',response:r.result.content,history:[]}});}});`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	res, err := p.Run(ctx, Request{RunID: "test"}, func(context.Context, ToolCall) (ToolResult, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return ToolResult{Content: "hello"}, nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Response != "hello" || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d", res, calls.Load())
	}
}

func TestPiHostDisablesProfileForLegacyWorkers(t *testing.T) {
	p := fixtureWorker(t, `input.on('line', line => {
  const record = JSON.parse(line);
  if (record.type !== 'run') return;
  if (record.request.profile.enabled !== false || record.request.profile.instruction !== undefined) {
    process.exit(1);
  }
  send({type:'result',result:{status:'completed',response:record.request.systemPrompt,history:[]}});
});`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const prompt = "Return the fixture result."
	res, err := p.Run(ctx, Request{SystemPrompt: prompt}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Response != prompt {
		t.Fatalf("expected the original system prompt, got %q", res.Response)
	}
}

func TestPiCancelReachesHostToolWithoutCancelingParent(t *testing.T) {
	p := fixtureWorker(t, `input.on('line',line=>{const r=JSON.parse(line);if(r.type==='run')send({type:'tool_call',call:{id:'wait',name:'wait',arguments:{}}});
if(r.type==='cancel')send({type:'result',result:{status:'canceled',response:'partial',history:[]}});});`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var control Control
	started := make(chan struct{})
	stopped := make(chan struct{})
	go func() { <-started; _ = control.Cancel() }()
	res, err := p.Run(ctx, Request{RunID: "test"}, func(ctx context.Context, _ ToolCall) (ToolResult, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ToolResult{}, ctx.Err()
	}, nil, func(c Control) func() { control = c; return func() {} })
	if !errors.Is(err, context.Canceled) || res == nil || res.Response != "partial" {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("host tool was not canceled")
	}
	if ctx.Err() != nil {
		t.Fatal("parent context should remain active")
	}
}

func TestPiUnexpectedExitIsNotSuccess(t *testing.T) {
	p := fixtureWorker(t, `input.on('line',()=>process.exit(0));`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Run(ctx, Request{}, nil, nil, nil); err == nil {
		t.Fatal("worker exit without final result was accepted")
	}
}

func TestSessionLockWaitRespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireSession(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = acquireSession(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
