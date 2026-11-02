package assessment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/openai"
)

type Failure struct {
	Kind string
	Err  error
}

func (f *Failure) Error() string { return f.Kind + ": " + f.Err.Error() }
func (f *Failure) Unwrap() error { return f.Err }

var statusCodePattern = regexp.MustCompile(`(?i)(?:status(?:[_ ]code)?\s*[:= ]\s*|http\s+|\s)(400|401|403|408|429|500|502|503|504)(?:\s|:|,|$)`)

func failureKind(err error) string {
	if err == nil {
		return ""
	}
	var f *Failure
	if errors.As(err, &f) {
		return f.Kind
	}
	if errors.Is(err, ErrBudget) {
		return "budget"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var n net.Error
	if errors.As(err, &n) && n.Timeout() {
		return "model_transient"
	}
	var api *openai.APIError
	code := 0
	if errors.As(err, &api) {
		code = api.StatusCode
	} else {
		m := statusCodePattern.FindStringSubmatch(err.Error())
		if len(m) > 1 {
			fmt.Sscan(m[1], &code)
		}
	}
	switch code {
	case 401, 403:
		return "model_auth"
	case 408, 429, 500, 502, 503, 504:
		return "model_transient"
	case 400:
		return "model_request"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "model_transient"
	}
	return "execution_error"
}
func (e *Engine) call(ctx context.Context, id string, msgs []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
	for attempt := 0; ; attempt++ {
		reply, err := e.callOnce(ctx, id, msgs, tools)
		if err == nil {
			return reply, nil
		}
		kind := failureKind(err)
		if kind != "model_transient" || attempt >= 2 || ctx.Err() != nil {
			return nil, &Failure{kind, err}
		}
		if err = e.Store.Change(id, "", "model_retry", "Temporary model failure; retrying model request only", func(t *Task) error { t.ModelRetries++; return nil }); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
}

type Attempt struct {
	Material      string `json:"material_fingerprint"`
	Cycle         int    `json:"retry_cycle"`
	Action        string `json:"action"`
	Outcome       string `json:"outcome"`
	EvidenceCount int    `json:"evidence_count"`
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func actionFingerprint(c agent.ToolCall) string {
	return digest(struct {
		Name      string
		Arguments map[string]interface{}
	}{strings.ToLower(c.Function.Name), c.Function.Arguments})
}
func observationFingerprint(result *agent.ToolExecutionResult) string {
	return digest(struct {
		Text           string
		Error, Blocked bool
	}{strings.TrimSpace(result.Result), result.IsError, result.Blocked})
}
func stagnant(s *Step, call agent.ToolCall) bool {
	name := strings.ToLower(call.Function.Name)
	if strings.Contains(name, "wait") || strings.Contains(name, "poll") || strings.Contains(name, "get_execution") {
		return false
	}
	key := actionFingerprint(call)
	last := ""
	count := 0
	for i := len(s.Attempts) - 1; i >= 0; i-- {
		a := s.Attempts[i]
		if a.Cycle != s.RetryCount {
			continue
		}
		if a.Action != key {
			continue
		}
		if a.EvidenceCount != len(s.Evidence) {
			return false
		}
		if last == "" {
			last = a.Outcome
		}
		if a.Outcome != last {
			return false
		}
		count++
		if count >= 2 {
			return true
		}
	}
	return false
}
func toolFailure(result *agent.ToolExecutionResult) string {
	if result.Blocked {
		return "policy_denied"
	}
	if !result.IsError {
		return ""
	}
	text := strings.ToLower(result.Result)
	if strings.Contains(text, "unauthorized") || strings.Contains(text, "authentication") || strings.Contains(text, "登录") || strings.Contains(text, "401") || strings.Contains(text, "403") {
		return "target_auth"
	}
	if strings.Contains(text, "timeout") || strings.Contains(text, "超时") || strings.Contains(text, "cancel") || strings.Contains(text, "终止") {
		return "operation_uncertain"
	}
	return ""
}

type BudgetChange struct {
	TokenBudget      int   `json:"token_budget"`
	RemainingSeconds int64 `json:"remaining_seconds"`
	MaxRounds        int   `json:"max_rounds"`
}

func (e *Engine) AdjustBudget(id string, b BudgetChange) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active[id] != nil {
		return ErrConflict
	}
	return e.Store.Change(id, "", "budget_adjusted", "User adjusted remaining budget", func(t *Task) error {
		if !contains([]string{"draft", "paused", "blocked", "partial", "exhausted", "failed"}, t.Status) {
			return ErrConflict
		}
		if b.TokenBudget < t.Tokens+t.Reserved+1000 || b.TokenBudget < 4000 || b.TokenBudget > 10000000 || b.RemainingSeconds < 60 || b.RemainingSeconds > 86400 || b.MaxRounds <= t.Rounds || b.MaxRounds > 50 {
			return fmt.Errorf("budget must cover usage, provide 60..86400 seconds, and allow additional planning rounds (max 50)")
		}
		t.TokenBudget = b.TokenBudget
		t.RemainingSeconds = b.RemainingSeconds
		t.MaxRounds = b.MaxRounds
		t.Status = "paused"
		t.Reason = "budget_adjusted"
		return nil
	}, b)
}

func observationAsset(call agent.ToolCall) string {
	for _, key := range []string{"url", "target", "host", "ip", "domain", "endpoint"} {
		if value, ok := call.Function.Arguments[key].(string); ok {
			return clip(value, 500)
		}
	}
	return ""
}

func stagnantInTask(t *Task, s *Step, call agent.ToolCall) bool {
	if stagnant(s, call) {
		return true
	}
	if s.VerifyOf != "" || s.Profile == "verification" {
		return false
	}
	for _, other := range t.Steps {
		if other.ID == s.ID || other.FailureKind != "stagnation" {
			continue
		}
		if len(other.Attempts) > 0 && other.Attempts[len(other.Attempts)-1].Material == digest(t.Material) && stagnant(other, call) {
			return true
		}
	}
	return false
}
