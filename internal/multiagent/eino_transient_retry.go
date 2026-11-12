package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultEinoRunRetryMaxAttempts = 4
	defaultEinoRunRetryMaxBackoff  = 30 * time.Second
)

var httpStatusInErrorPattern = regexp.MustCompile(`(?i)(?:http|status(?:\s+code)?|upstream\s+returned)\s*[:=]?\s*(\d{3})\b`)

// isEinoTransientRunError 是 Eino 运行期「可退避重试 vs 直接失败」的唯一判据。
// 429/5xx/网络抖动等返回 true；用户取消、超时、迭代上限、鉴权失败等返回 false。
// 其它模块（run loop、summarization 等）只调用本函数，不在别处维护平行规则。
func isEinoTransientRunError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, adk.ErrExceedMaxRetries) {
		return false
	}
	if _, ok := isEinoNativeWillRetry(err); ok {
		return false
	}
	if isEinoIterationLimitError(err) {
		return false
	}
	err = unwrapEinoRetryExhausted(err)
	var apiErr *einoopenai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
		return isRetryableHTTPStatus(apiErr.HTTPStatusCode)
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if isEinoEmptySummaryContentErrorText(msg) {
		return true
	}
	if status := httpStatusFromErrorText(msg); status > 0 {
		return isRetryableHTTPStatus(status)
	}
	transientMarkers := []string{
		"too many requests",
		"rate limit",
		"rate_limit",
		"ratelimit",
		"overloaded",
		"capacity",
		"temporarily unavailable",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"internal server error",
		"unexpected internal error",
		"connection reset",
		"connection refused",
		"connection closed",
		"i/o timeout",
		"no such host",
		"network is unreachable",
		"broken pipe",
		"read tcp",
		"write tcp",
		"dial tcp",
		"tls handshake timeout",
		"stream error",
		"failed to receive stream chunk",
		"goaway", // http2: server sent GOAWAY and closed the connection
		"unexpected eof",
		`": eof`, // net/http: Post "url": EOF (often wraps io.EOF)
		"unexpected end of json",
	}
	for _, m := range transientMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

func isEinoEmptySummaryContentErrorText(msg string) bool {
	return strings.Contains(msg, "summary content is empty") ||
		strings.Contains(msg, "agentic summarization returned empty summary")
}

func isRetryableHTTPStatus(status int) bool {
	switch status {
	case 408, 409, 425, 429:
		return true
	default:
		return status >= 500 && status <= 599
	}
}

func einoTransientRunErrorUserDetail(err error) (kind, summary string) {
	if err == nil {
		return "", ""
	}
	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)
	if status := httpStatusFromErrorText(lower); status > 0 {
		switch {
		case status == 429:
			kind = "rate_limit"
		case status == 408 || status == 409 || status == 425:
			kind = "retryable_http"
		case status >= 500 && status <= 599:
			kind = "upstream_server"
		default:
			kind = "http_error"
		}
	} else {
		var apiErr *einoopenai.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
			switch {
			case apiErr.HTTPStatusCode == 429:
				kind = "rate_limit"
			case apiErr.HTTPStatusCode == 408 || apiErr.HTTPStatusCode == 409 || apiErr.HTTPStatusCode == 425:
				kind = "retryable_http"
			case apiErr.HTTPStatusCode >= 500 && apiErr.HTTPStatusCode <= 599:
				kind = "upstream_server"
			default:
				kind = "http_error"
			}
		}
	}
	if kind == "" {
		switch {
		case strings.Contains(lower, "too many requests") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "rate_limit") ||
			strings.Contains(lower, "ratelimit"):
			kind = "rate_limit"
		case strings.Contains(lower, "overloaded") ||
			strings.Contains(lower, "capacity") ||
			strings.Contains(lower, "temporarily unavailable") ||
			strings.Contains(lower, "service unavailable") ||
			strings.Contains(lower, "unexpected internal error"):
			kind = "upstream_busy"
		case strings.Contains(lower, "connection reset") ||
			strings.Contains(lower, "connection refused") ||
			strings.Contains(lower, "connection closed") ||
			strings.Contains(lower, "i/o timeout") ||
			strings.Contains(lower, "no such host") ||
			strings.Contains(lower, "network is unreachable") ||
			strings.Contains(lower, "broken pipe") ||
			strings.Contains(lower, "read tcp") ||
			strings.Contains(lower, "write tcp") ||
			strings.Contains(lower, "dial tcp") ||
			strings.Contains(lower, "tls handshake timeout") ||
			strings.Contains(lower, "goaway") ||
			strings.Contains(lower, "unexpected eof"):
			kind = "network"
		case strings.Contains(lower, "stream error") ||
			strings.Contains(lower, "failed to receive stream chunk") ||
			strings.Contains(lower, "unexpected end of json"):
			kind = "stream"
		default:
			kind = "transient"
		}
	}
	return kind, einoTrimRetryErrorSummary(msg)
}

func einoTrimRetryErrorSummary(msg string) string {
	msg = strings.Join(strings.Fields(strings.TrimSpace(msg)), " ")
	const maxRunes = 500
	runes := []rune(msg)
	if len(runes) <= maxRunes {
		return msg
	}
	return string(runes[:maxRunes]) + "..."
}

func httpStatusFromErrorText(msg string) int {
	match := httpStatusInErrorPattern.FindStringSubmatch(msg)
	if len(match) != 2 {
		return 0
	}
	status, _ := strconv.Atoi(match[1])
	return status
}

type einoTransientRunRetryPolicy struct {
	maxAttempts int
	maxBackoff  time.Duration
}

func einoTransientRunRetryPolicyFromArgs(args *einoADKRunLoopArgs) einoTransientRunRetryPolicy {
	return einoTransientRunRetryPolicy{
		maxAttempts: einoRunRetryMaxAttempts(args),
		maxBackoff:  einoRunRetryMaxBackoff(args),
	}
}

func einoTransientRunRetryPolicyFromMW(mw *config.MultiAgentEinoMiddlewareConfig) einoTransientRunRetryPolicy {
	return einoTransientRunRetryPolicy{
		maxAttempts: RunRetryMaxAttemptsFromConfig(mw),
		maxBackoff:  einoRunRetryMaxBackoffFromConfig(mw),
	}
}

// einoTransientRunRetrier 在 run loop 内对临时错误做指数退避并重启 Runner（唯一重试执行层）。
type einoTransientRunRetrier struct {
	policy   einoTransientRunRetryPolicy
	attempts int
}

func newEinoTransientRunRetrier(policy einoTransientRunRetryPolicy) *einoTransientRunRetrier {
	return &einoTransientRunRetrier{policy: policy}
}

// tryRetry 对临时错误退避后返回重启消息；次数用尽返回 exhausted 错误。
func (r *einoTransientRunRetrier) tryRetry(
	ctx context.Context,
	runErr error,
	args *einoADKRunLoopArgs,
	baseMsgs, accumulated []adk.Message,
	baseCount int,
) (restarted bool, restartMsgs []adk.Message, ctxSource einoRunRestartContextSource, backoff time.Duration, fatal error) {
	if runErr == nil || !isEinoTransientRunError(runErr) {
		return false, nil, "", 0, runErr
	}
	r.attempts++
	if r.attempts > r.policy.maxAttempts {
		return false, nil, "", 0, fmt.Errorf("transient retry exhausted after %d attempts: %w", r.policy.maxAttempts, runErr)
	}
	backoff = einoTransientRetryBackoff(r.attempts-1, r.policy.maxBackoff)
	select {
	case <-ctx.Done():
		return false, nil, "", 0, ctx.Err()
	case <-time.After(backoff):
	}
	restartMsgs, ctxSource = einoMessagesForRunRestart(args, baseMsgs, accumulated, baseCount)
	return true, restartMsgs, ctxSource, backoff, nil
}

func (r *einoTransientRunRetrier) attempt() int { return r.attempts }

func (r *einoTransientRunRetrier) maxAttempts() int { return r.policy.maxAttempts }

// reset 在退避重试后成功推进（流/消息完整接收）时清零计数，使后续临时错误从第 1 次退避重新开始。
func (r *einoTransientRunRetrier) reset() { r.attempts = 0 }

func einoRunRetryMaxAttempts(args *einoADKRunLoopArgs) int {
	if args != nil && args.RunRetryMaxAttempts > 0 {
		return args.RunRetryMaxAttempts
	}
	return defaultEinoRunRetryMaxAttempts
}

// RunRetryMaxAttemptsFromConfig returns the native model retry count, with legacy run_retry_max_attempts as a fallback.
func RunRetryMaxAttemptsFromConfig(mw *config.MultiAgentEinoMiddlewareConfig) int {
	if mw != nil {
		if mw.ModelRetryMaxRetries > 0 {
			return mw.ModelRetryMaxRetries
		}
		if mw.RunRetryMaxAttempts > 0 {
			return mw.RunRetryMaxAttempts
		}
	}
	return defaultEinoRunRetryMaxAttempts
}

func einoRunRetryMaxBackoff(args *einoADKRunLoopArgs) time.Duration {
	if args != nil && args.RunRetryMaxBackoffSec > 0 {
		return time.Duration(args.RunRetryMaxBackoffSec) * time.Second
	}
	return defaultEinoRunRetryMaxBackoff
}

func einoRunRetryMaxBackoffFromConfig(mw *config.MultiAgentEinoMiddlewareConfig) time.Duration {
	if mw != nil {
		if mw.ModelRetryMaxBackoffSec > 0 {
			return time.Duration(mw.ModelRetryMaxBackoffSec) * time.Second
		}
		if mw.RunRetryMaxBackoffSec > 0 {
			return time.Duration(mw.RunRetryMaxBackoffSec) * time.Second
		}
	}
	return defaultEinoRunRetryMaxBackoff
}

// einoRunRestartContextSource 描述无 checkpoint Resume 时 Run 使用的消息来源（日志/SSE）。
type einoRunRestartContextSource string

const (
	einoRestartContextInitial     einoRunRestartContextSource = "initial"
	einoRestartContextAccumulated einoRunRestartContextSource = "accumulated"
	einoRestartContextModelTrace  einoRunRestartContextSource = "model_trace"
)

// einoMessagesForRunRestart 在退避后重新 Run 时选用最完整的上下文：
// 1) ModelFacingTrace（与模型实际入参一致） 2) 事件流累积的 runAccumulatedMsgs 3) 初始 msgs。
func einoMessagesForRunRestart(args *einoADKRunLoopArgs, baseMsgs, accumulated []adk.Message, baseCount int) ([]adk.Message, einoRunRestartContextSource) {
	if trace := modelFacingTraceSnapshot(args); len(trace) > 0 {
		// Drop regenerated instructions while preserving the host-owned human ledger.
		return messagesForRestartWithHumanLedger(args, trace), einoRestartContextModelTrace
	}
	if len(accumulated) > baseCount {
		return messagesForRestartWithHumanLedger(args, accumulated), einoRestartContextAccumulated
	}
	return messagesForRestartWithHumanLedger(args, baseMsgs), einoRestartContextInitial
}

// messagesForRestartWithHumanLedger removes stale system instructions and project
// indexes. Only structured host provenance can survive a restart; text that merely
// resembles a ledger is not a source of original human input.
func messagesForRestartWithHumanLedger(args *einoADKRunLoopArgs, messages []adk.Message) []adk.Message {
	var entries []string
	for _, msg := range messages {
		if msg == nil || msg.Role != schema.System {
			continue
		}
		if _, present := msg.Extra[userIntentLedgerExtraKey]; present {
			// Match system normalization: the latest canonical ledger is authoritative,
			// including an empty/malformed value that must not revive an older ledger.
			entries = trustedUserIntentLedgerEntries(msg)
		}
	}
	out := stripADKSystemMessages(messages)
	if len(entries) == 0 {
		return out
	}
	maxRunes := config.DefaultSummarizationUserIntentLedgerMaxRunes
	entryMax := config.DefaultSummarizationUserIntentLedgerEntryMaxRunes
	maxTotalTokens := 0
	if args != nil {
		maxTotalTokens = args.MaxTotalTokens
		if args.MiddlewareConfig != nil {
			maxRunes = args.MiddlewareConfig.SummarizationUserIntentLedgerMaxRunesEffective()
			entryMax = args.MiddlewareConfig.SummarizationUserIntentLedgerEntryMaxRunesEffective()
		}
	}
	maxRunes = minPositiveInt(maxRunes, modelFacingRuneBudget(maxTotalTokens, 0.20))
	anchor := schema.SystemMessage("")
	anchor.Extra = map[string]interface{}{userIntentLedgerExtraKey: entries}
	ledger := buildOriginalUserIntentLedgerMessage([]adk.Message{anchor}, maxRunes, minPositiveInt(entryMax, maxRunes))
	return mergeMessageIntoLeadingSystem(out, ledger)
}

// adkMessagesHasUserContent reports whether the conversation tail is already a user turn
// with the given content. Only the last message counts: matching text in an earlier round
// (e.g. user repeats the same prompt after an assistant reply) must not suppress appending
// the new user turn — Claude 4.6+ rejects requests whose final message is assistant.
func adkMessagesHasUserContent(msgs []adk.Message, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	if last == nil || last.Role != schema.User {
		return false
	}
	return strings.TrimSpace(last.Content) == want
}

// appendUserMessageIfNeeded 在 history 轨迹之后追加本轮 user 消息（仅当尾部已是相同 user 句）。
func appendUserMessageIfNeeded(msgs []adk.Message, userMessage string) []adk.Message {
	if strings.TrimSpace(userMessage) == "" || adkMessagesHasUserContent(msgs, userMessage) {
		return msgs
	}
	return append(msgs, schema.UserMessage(userMessage))
}

// einoTransientRetryBackoff uses equal-jitter exponential backoff. Jitter avoids
// synchronized retries when many conversations hit the same provider limit.
func einoTransientRetryBackoff(attempt int, maxBackoff time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 30 {
		attempt = 30
	}
	ceiling := time.Duration(1<<uint(attempt+1)) * time.Second
	if maxBackoff > 0 && ceiling > maxBackoff {
		ceiling = maxBackoff
	}
	if ceiling <= 1 {
		return ceiling
	}
	half := ceiling / 2
	return half + time.Duration(rand.Int64N(int64(ceiling-half)+1))
}
