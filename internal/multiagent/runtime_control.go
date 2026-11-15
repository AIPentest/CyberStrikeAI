package multiagent

import "context"

type agentRuntimeCancelRegistrarKey struct{}
type agentTurnLoopInterruptRegistrarKey struct{}

// AgentRuntimeCancelRegistrar binds the currently active Pi cancel hook
// into the host task manager. The hook returns true when Agent accepted and
// handled the cancel request, so the host can avoid canceling the parent context.
type AgentRuntimeCancelRegistrar func(cancel func(error) bool) (unregister func())

// WithAgentRuntimeCancelRegistrar lets the HTTP/task layer trigger Agent's native
// Agent Cancel before falling back to the existing context cancellation path.
func WithAgentRuntimeCancelRegistrar(ctx context.Context, registrar AgentRuntimeCancelRegistrar) context.Context {
	if ctx == nil || registrar == nil {
		return ctx
	}
	return context.WithValue(ctx, agentRuntimeCancelRegistrarKey{}, registrar)
}

func agentRuntimeCancelRegistrarFromContext(ctx context.Context) AgentRuntimeCancelRegistrar {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(agentRuntimeCancelRegistrarKey{}).(AgentRuntimeCancelRegistrar); ok {
		return v
	}
	return nil
}

// AgentTurnLoopInterruptRegistrar binds a conversation-level TurnLoop interrupt
// pusher into the host task manager. The pusher receives the user supplied note
// and returns true when the note was accepted by the loop.
type AgentTurnLoopInterruptRegistrar func(push func(note string) bool) (unregister func())

// WithAgentTurnLoopInterruptRegistrar lets the HTTP/task layer enqueue a user
// supplement into an active Agent TurnLoop before falling back to cancellation.
func WithAgentTurnLoopInterruptRegistrar(ctx context.Context, registrar AgentTurnLoopInterruptRegistrar) context.Context {
	if ctx == nil || registrar == nil {
		return ctx
	}
	return context.WithValue(ctx, agentTurnLoopInterruptRegistrarKey{}, registrar)
}

func agentTurnLoopInterruptRegistrarFromContext(ctx context.Context) AgentTurnLoopInterruptRegistrar {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(agentTurnLoopInterruptRegistrarKey{}).(AgentTurnLoopInterruptRegistrar); ok {
		return v
	}
	return nil
}
