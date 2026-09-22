package multiagent

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type hitlInterceptorKey struct{}

type HITLToolInterceptor func(ctx context.Context, toolName, arguments string) (string, error)

type humanRejectError struct {
	reason string
}

func (e *humanRejectError) Error() string {
	if strings.TrimSpace(e.reason) == "" {
		return "rejected by user"
	}
	return "rejected by user: " + strings.TrimSpace(e.reason)
}

func NewHumanRejectError(reason string) error {
	return &humanRejectError{reason: strings.TrimSpace(reason)}
}

func IsHumanRejectError(err error) bool {
	var target *humanRejectError
	return errors.As(err, &target)
}

func WithHITLToolInterceptor(ctx context.Context, fn HITLToolInterceptor) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, hitlInterceptorKey{}, fn)
}

// hitlToolCallMiddleware 同时注册 Invokable 与 Streamable。
// Eino filesystem 的 execute 为流式工具（StreamableTool），仅挂 Invokable 时人机协同不会拦截，会直接执行。
func hitlToolCallMiddleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable:  hitlInvokableToolCallMiddleware(),
		Streamable: hitlStreamableToolCallMiddleware(),
	}
}

func hitlClearReturnDirectlyIfTransfer(ctx context.Context, toolName string) {
	if !strings.EqualFold(strings.TrimSpace(toolName), adk.TransferToAgentToolName) {
		return
	}
	_ = clearADKReturnDirectly(ctx)
}

func hitlEditedArgumentsNotice(original, edited string) string {
	original = strings.TrimSpace(original)
	edited = strings.TrimSpace(edited)
	if edited == "" || edited == original {
		return ""
	}
	return "[HITL] Human reviewer approved this tool call with edited arguments.\n" +
		"Original arguments: " + original + "\n" +
		"Executed arguments: " + edited + "\n\n"
}

func hitlPrependEditedArgumentsNotice(result, original, edited string) string {
	notice := hitlEditedArgumentsNotice(original, edited)
	if notice == "" {
		return result
	}
	return notice + result
}

func hitlCollectStringStream(sr *schema.StreamReader[string]) (string, error) {
	if sr == nil {
		return "", nil
	}
	defer sr.Close()
	var b strings.Builder
	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return b.String(), nil
		}
		if err != nil {
			return b.String(), err
		}
		b.WriteString(chunk)
	}
}

func hitlInvokableToolCallMiddleware() compose.InvokableToolMiddleware {
	return func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
			originalArgs := ""
			editedArgs := ""
			if input != nil {
				if fn, ok := ctx.Value(hitlInterceptorKey{}).(HITLToolInterceptor); ok && fn != nil {
					originalArgs = input.Arguments
					edited, err := fn(ctx, input.Name, input.Arguments)
					if err != nil {
						if IsHumanRejectError(err) {
							// Human rejection should be a soft tool result so the model can continue iterating.
							// tool_search 须保持 JSON，否则 Eino toolsearch 中间件解析历史时会硬崩 ChatModel。
							msg := HitlRejectToolResult(input.Name, err.Error())
							// transfer_to_agent 在 Eino 中标记为 returnDirectly：工具成功后 ReAct 子图会直接 END，
							// 并依赖真实工具内的 SendToolGenAction 触发移交。HITL 拒绝时不会执行真实工具，
							// 若仍走 returnDirectly 分支，监督者会在无 Transfer 动作的情况下结束，模型不再迭代。
							hitlClearReturnDirectlyIfTransfer(ctx, input.Name)
							return &compose.ToolOutput{Result: msg}, nil
						}
						return nil, err
					}
					if edited != "" {
						editedArgs = edited
						input.Arguments = edited
					}
				}
			}
			out, err := next(ctx, input)
			if err != nil || out == nil {
				return out, err
			}
			out.Result = hitlPrependEditedArgumentsNotice(out.Result, originalArgs, editedArgs)
			return out, nil
		}
	}
}

func hitlStreamableToolCallMiddleware() compose.StreamableToolMiddleware {
	return func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
			originalArgs := ""
			editedArgs := ""
			if input != nil {
				if fn, ok := ctx.Value(hitlInterceptorKey{}).(HITLToolInterceptor); ok && fn != nil {
					originalArgs = input.Arguments
					edited, err := fn(ctx, input.Name, input.Arguments)
					if err != nil {
						if IsHumanRejectError(err) {
							msg := HitlRejectToolResult(input.Name, err.Error())
							hitlClearReturnDirectlyIfTransfer(ctx, input.Name)
							return &compose.StreamToolOutput{
								Result: schema.StreamReaderFromArray([]string{msg}),
							}, nil
						}
						return nil, err
					}
					if edited != "" {
						editedArgs = edited
						input.Arguments = edited
					}
				}
			}
			out, err := next(ctx, input)
			if err != nil || out == nil {
				return out, err
			}
			if hitlEditedArgumentsNotice(originalArgs, editedArgs) == "" {
				return out, nil
			}
			result, collectErr := hitlCollectStringStream(out.Result)
			if collectErr != nil {
				return nil, collectErr
			}
			out.Result = schema.StreamReaderFromArray([]string{
				hitlPrependEditedArgumentsNotice(result, originalArgs, editedArgs),
			})
			return out, nil
		}
	}
}
