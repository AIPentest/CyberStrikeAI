package multiagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type agenticShapedReactState struct {
	ToolGenActions map[string]*adk.AgentAction
}

func TestSendADKToolGenActionWritesAgenticShapedState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	chain := compose.NewChain[string, string](compose.WithGenLocalState(func(context.Context) *agenticShapedReactState {
		return &agenticShapedReactState{}
	}))
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
		if err := sendADKToolGenAction(ctx, adk.TransferToAgentToolName, adk.NewTransferToAgentAction("expert")); err != nil {
			return "", err
		}
		return in, compose.ProcessState(ctx, func(_ context.Context, st *agenticShapedReactState) error {
			action := st.ToolGenActions[adk.TransferToAgentToolName]
			if action == nil || action.TransferToAgent == nil || action.TransferToAgent.DestAgentName != "expert" {
				return errors.New("missing transfer tool gen action")
			}
			return nil
		})
	}))
	r, err := chain.Compile(ctx)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := r.Invoke(ctx, "ok"); err != nil {
		t.Fatalf("invoke: %v", err)
	}
}

func TestClearADKReturnDirectlyZerosExportedFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	chain := compose.NewChain[string, string](compose.WithGenLocalState(func(context.Context) *adk.State {
		return &adk.State{HasReturnDirectly: true, ReturnDirectlyToolCallID: "call-1"}
	}))
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
		if err := clearADKReturnDirectly(ctx); err != nil {
			return "", err
		}
		return in, compose.ProcessState(ctx, func(_ context.Context, st *adk.State) error {
			if st.HasReturnDirectly || st.ReturnDirectlyToolCallID != "" || st.ReturnDirectlyEvent != nil {
				return errors.New("return-directly fields were not cleared")
			}
			return nil
		})
	}))
	r, err := chain.Compile(ctx)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := r.Invoke(ctx, "ok"); err != nil {
		t.Fatalf("invoke: %v", err)
	}
}

func TestAgenticExitToolDoesNotFailOnAgenticChatModelAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fakeModel := &capturingAgenticChatModel{
		output: agenticAssistantToolCall("tool-call-1", "exit", `{"final_result":"This is the final result"}`),
	}
	agent, err := newEinoAgenticChatModelAgentAdapter(ctx, einoAgenticChatModelAgentConfig{
		Name:        "agentic-exit",
		Description: "exit regression",
		Instruction: "finish with exit",
		Model:       fakeModel,
		Exit:        &adk.ExitTool{},
	})
	if err != nil {
		t.Fatalf("newEinoAgenticChatModelAgentAdapter: %v", err)
	}

	var sawExit bool
	var exitContent string
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("please exit")}})
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			t.Fatalf("agent event error: %v", ev.Err)
		}
		if ev.Action != nil && ev.Action.Exit {
			sawExit = true
			if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.Message != nil {
				exitContent = ev.Output.MessageOutput.Message.Content
			}
		}
		if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.Message != nil {
			msg := ev.Output.MessageOutput.Message
			if strings.Contains(msg.Content, "cannot find state with type") {
				t.Fatalf("exit still failed with classic state mismatch: %q", msg.Content)
			}
		}
	}
	if !sawExit {
		t.Fatal("expected Exit action on agentic ChatModelAgent")
	}
	if exitContent != "This is the final result" {
		t.Fatalf("exit content = %q, want final result", exitContent)
	}
}

func TestAgenticBuiltinActionMiddlewareInterceptsTransfer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	nextCalled := false
	mw := agenticBuiltinActionToolMiddleware()
	endpoint := mw.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		nextCalled = true
		return nil, errors.New("official transfer tool should not run")
	})
	chain := compose.NewChain[string, string](compose.WithGenLocalState(func(context.Context) *agenticShapedReactState {
		return &agenticShapedReactState{}
	}))
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
		out, err := endpoint(ctx, &compose.ToolInput{
			Name:      adk.TransferToAgentToolName,
			Arguments: `{"agent_name":"expert"}`,
		})
		if err != nil {
			return "", err
		}
		if out == nil || !strings.Contains(out.Result, "expert") {
			return "", errors.New("missing transfer result")
		}
		if nextCalled {
			return "", errors.New("official transfer tool ran")
		}
		return in, nil
	}))
	r, err := chain.Compile(ctx)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := r.Invoke(ctx, "ok"); err != nil {
		t.Fatalf("invoke: %v", err)
	}
}
