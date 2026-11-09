package multiagent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type einoTurnLoopRuntimeControl interface {
	Run(context.Context)
	PushInterruptContinue(string) bool
	StopImmediate(string)
	StopWhenIdle()
	Wait() *adk.TurnLoopExitState[EinoTurnLoopItem, *schema.Message]
}

type einoTurnLoopRuntimeFactory func(EinoTurnLoopRuntimeConfig) einoTurnLoopRuntimeControl

type einoTurnLoopIteratorStarterConfig struct {
	Context                     context.Context
	Agent                       adk.Agent
	ConversationID              string
	OrchMode                    string
	Progress                    func(eventType, message string, data interface{})
	Logger                      *zap.Logger
	Store                       adk.CheckPointStore
	CheckPointID                string
	InterruptTimeout            time.Duration
	NativeCancelCause           *atomic.Value
	UnregisterAgentCancel       *func()
	UnregisterTurnLoopInterrupt *func()
	RuntimeCancelRegistrar      AgentRuntimeCancelRegistrar
	TurnLoopInterruptRegistrar  AgentTurnLoopInterruptRegistrar
	RuntimeFactory              einoTurnLoopRuntimeFactory
}

type einoTurnLoopIteratorStarter struct {
	cfg einoTurnLoopIteratorStarterConfig
}

func newEinoTurnLoopIteratorStarter(cfg einoTurnLoopIteratorStarterConfig) *einoTurnLoopIteratorStarter {
	if cfg.RuntimeFactory == nil {
		cfg.RuntimeFactory = func(runtimeCfg EinoTurnLoopRuntimeConfig) einoTurnLoopRuntimeControl {
			return NewEinoTurnLoopRuntime(runtimeCfg)
		}
	}
	return &einoTurnLoopIteratorStarter{cfg: cfg}
}

func (s *einoTurnLoopIteratorStarter) Start(runMsgs []adk.Message) *adk.AsyncIterator[*adk.AgentEvent] {
	if s == nil {
		return nil
	}
	callAndClearUnregister(s.cfg.UnregisterTurnLoopInterrupt)
	callAndClearUnregister(s.cfg.UnregisterAgentCancel)

	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	eventsBridge := newEinoTurnLoopEventBridge(s.cfg.ConversationID, s.cfg.OrchMode, s.cfg.Progress, gen)
	runtime := s.cfg.RuntimeFactory(EinoTurnLoopRuntimeConfig{
		Agent:            s.cfg.Agent,
		InitialMessages:  runMsgs,
		Store:            s.cfg.Store,
		CheckpointID:     s.turnLoopCheckpointID(),
		EnableStreaming:  true,
		InterruptTimeout: s.cfg.InterruptTimeout,
		OnAgentEvents:    eventsBridge.OnAgentEvents,
	})
	s.bindTurnLoopInterrupt(runtime)
	s.bindRuntimeCancel(runtime)
	runtime.Run(s.cfg.Context)
	runtime.StopWhenIdle()
	go func() {
		defer gen.Close()
		state := runtime.Wait()
		if state == nil || state.ExitReason == nil || eventsBridge.ForwardedError() {
			return
		}
		gen.Send(&adk.AgentEvent{Err: state.ExitReason})
	}()
	return iter
}

func (s *einoTurnLoopIteratorStarter) turnLoopCheckpointID() string {
	if s == nil || s.cfg.CheckPointID == "" {
		return ""
	}
	return buildEinoTurnLoopCheckpointID(s.cfg.OrchMode)
}

func (s *einoTurnLoopIteratorStarter) bindTurnLoopInterrupt(runtime einoTurnLoopRuntimeControl) {
	if s == nil || runtime == nil || s.cfg.TurnLoopInterruptRegistrar == nil || s.cfg.UnregisterTurnLoopInterrupt == nil {
		return
	}
	*s.cfg.UnregisterTurnLoopInterrupt = s.cfg.TurnLoopInterruptRegistrar(func(note string) bool {
		ok := runtime.PushInterruptContinue(note)
		if ok {
			s.emitInterruptContinueProgress(note)
		}
		return ok
	})
}

func (s *einoTurnLoopIteratorStarter) bindRuntimeCancel(runtime einoTurnLoopRuntimeControl) {
	if s == nil || runtime == nil || s.cfg.RuntimeCancelRegistrar == nil || s.cfg.UnregisterAgentCancel == nil {
		return
	}
	*s.cfg.UnregisterAgentCancel = s.cfg.RuntimeCancelRegistrar(func(cause error) bool {
		s.storeNativeCancelCause(cause)
		if errors.Is(cause, ErrInterruptContinue) {
			return runtime.PushInterruptContinue("")
		}
		runtime.StopImmediate("task_cancelled")
		if s.cfg.Logger != nil {
			s.cfg.Logger.Info("eino turn loop stop requested",
				zap.String("conversation_id", s.cfg.ConversationID),
				zap.String("orchestration", s.cfg.OrchMode),
				zap.Error(cause))
		}
		return true
	})
}

func (s *einoTurnLoopIteratorStarter) storeNativeCancelCause(cause error) {
	if s == nil || s.cfg.NativeCancelCause == nil || cause == nil {
		return
	}
	s.cfg.NativeCancelCause.Store(cause)
}

func (s *einoTurnLoopIteratorStarter) emitInterruptContinueProgress(note string) {
	if s == nil || s.cfg.Progress == nil {
		return
	}
	trimmed := strings.TrimSpace(note)
	s.cfg.Progress("user_interrupt_continue", einoTurnLoopInterruptTimelineSummary(note), map[string]interface{}{
		"conversationId": s.cfg.ConversationID,
		"rawReason":      trimmed,
		"emptyReason":    trimmed == "",
		"kind":           "turn_loop_preempt",
		"source":         "eino",
		"orchestration":  s.cfg.OrchMode,
	})
	s.cfg.Progress("progress", "User supplement pushed into Eino TurnLoop; waiting for the safe-point switch…", map[string]interface{}{
		"conversationId": s.cfg.ConversationID,
		"source":         "eino",
		"orchestration":  s.cfg.OrchMode,
	})
}

func callAndClearUnregister(target *func()) {
	if target == nil || *target == nil {
		return
	}
	(*target)()
	*target = nil
}
