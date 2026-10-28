package multiagent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// lenientPlan keeps plan_execute running even when model tool arguments contain minor JSON defects.
// It first tries strict JSON, then falls back to lightweight step extraction heuristics.
type lenientPlan struct {
	Steps []string `json:"steps"`
}

func newLenientPlan(context.Context) planexecute.Plan {
	return &lenientPlan{}
}

func (p *lenientPlan) FirstStep() string {
	if p == nil || len(p.Steps) == 0 {
		return ""
	}
	return p.Steps[0]
}

func (p *lenientPlan) MarshalJSON() ([]byte, error) {
	type alias lenientPlan
	return json.Marshal((*alias)(p))
}

func (p *lenientPlan) UnmarshalJSON(b []byte) error {
	type alias lenientPlan
	var strict alias
	if err := json.Unmarshal(b, &strict); err == nil {
		strict.Steps = normalizePlanSteps(strict.Steps)
		if len(strict.Steps) > 0 {
			*p = lenientPlan(strict)
			return nil
		}
	}

	steps := extractPlanStepsLenient(string(b))
	if len(steps) == 0 {
		steps = []string{"Continue with the next step for the current goal and output verifiable evidence."}
	}
	p.Steps = steps
	return nil
}

func extractPlanStepsLenient(raw string) []string {
	s := strings.TrimSpace(stripCodeFence(raw))
	if s == "" {
		return nil
	}

	if extracted, ok := sliceByStepsArray(s); ok {
		var arr []string
		if err := json.Unmarshal([]byte(extracted), &arr); err == nil {
			arr = normalizePlanSteps(arr)
			if len(arr) > 0 {
				return arr
			}
		}
		if arr := splitStepsHeuristically(strings.Trim(extracted, "[]")); len(arr) > 0 {
			return arr
		}
	}

	// Last-resort: treat plaintext body as one actionable step.
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return []string{s}
}

func sliceByStepsArray(s string) (string, bool) {
	lower := strings.ToLower(s)
	key := `"steps"`
	i := strings.Index(lower, key)
	if i < 0 {
		return "", false
	}
	start := strings.Index(s[i:], "[")
	if start < 0 {
		return "", false
	}
	start += i
	depth := 0
	for j := start; j < len(s); j++ {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : j+1], true
			}
		}
	}
	return "", false
}

func splitStepsHeuristically(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\\n", "\n")
	var parts []string
	if strings.Contains(body, "\n") {
		for _, line := range strings.Split(body, "\n") {
			parts = append(parts, line)
		}
	} else {
		for _, seg := range strings.Split(body, ",") {
			parts = append(parts, seg)
		}
	}

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		t := strings.TrimSpace(part)
		t = strings.Trim(t, "\"'`")
		t = strings.TrimLeft(t, "-*0123456789.、 \t")
		t = strings.TrimSpace(strings.ReplaceAll(t, `\"`, `"`))
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return normalizePlanSteps(out)
}

func normalizePlanSteps(in []string) []string {
	out := make([]string, 0, len(in))
	for _, step := range in {
		t := strings.TrimSpace(step)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

