// Package toolguard applies configurable blocking rules before tool execution.
// It inspects tool names and arguments, including JSON string values and common
// percent escapes. It does not resolve hosts or inspect redirects, files, or
// arbitrary encoded payloads, and is not an exhaustive target authorization check.
package toolguard

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

const (
	MaxRules         = 100
	MaxIDLength      = 128
	MaxNameLength    = 200
	MaxPatternLength = 4096
	MaxMessageLength = 4096

	// The named group keeps surrounding boundary punctuation out of the reminder.
	governmentDomainPattern = `(?i)(?:^|[^\p{L}\p{M}\p{N}_.-])(?P<match>(?:(?:[\p{L}\p{M}\p{N}_*-]+\.)+gov(?:\.[\p{L}\p{M}\p{N}_*-]+)*|gov(?:\.[\p{L}\p{M}\p{N}_*-]+)+|\.gov(?:\.[\p{L}\p{M}\p{N}_*-]+)*)\.?)(?:$|[^\p{L}\p{M}\p{N}_.-])`
	defaultMessage          = "Identified {match}; the tool call was blocked by the security rule \"{rule}\". Please check the target and authorization scope, then try again."
)

type Config struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Rules   []Rule `json:"rules" yaml:"rules"`
}

type Rule struct {
	ID      string `json:"id" yaml:"id"`
	Name    string `json:"name" yaml:"name"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Pattern string `json:"pattern" yaml:"pattern"`
	Message string `json:"message" yaml:"message"`
}

type Match struct {
	RuleID      string `json:"ruleId"`
	RuleName    string `json:"ruleName"`
	MatchedText string `json:"matchedText"`
	Message     string `json:"message"`
}

type compiledRule struct {
	rule       Rule
	pattern    *regexp.Regexp
	matchGroup int
}

// Policy is an immutable snapshot safe for concurrent checks.
type Policy struct {
	config Config
	rules  []compiledRule
}

// DefaultConfig enables a conservative government-domain rule, including .gov,
// .gov.cn and wildcard forms such as *.gov.*. Additional rules can be configured.
func DefaultConfig() Config {
	return Config{
		Enabled: true,
		Rules: []Rule{{
			ID:      "government-domains",
			Name:    "Government sites protection",
			Enabled: true,
			Pattern: governmentDomainPattern,
			Message: "Identified {match}; attacking government sites is forbidden. Please check the target and authorization scope, and switch to an authorized non-government target.",
		}},
	}
}

// Compile validates every rule, including disabled ones. Limits are byte counts.
// Rule order determines precedence. An optional named (?P<match>...) group selects
// the text inserted into {match}; otherwise the entire regex match is used.
func Compile(config Config) (*Policy, error) {
	if len(config.Rules) > MaxRules {
		return nil, fmt.Errorf("tool guard: at most %d rules are allowed", MaxRules)
	}
	policy := &Policy{config: cloneConfig(config)}
	ids := make(map[string]struct{}, len(config.Rules))
	for i, rule := range policy.config.Rules {
		prefix := fmt.Sprintf("tool guard rule %d", i+1)
		if strings.TrimSpace(rule.ID) == "" || len(rule.ID) > MaxIDLength {
			return nil, fmt.Errorf("%s: id must be nonempty and at most %d bytes", prefix, MaxIDLength)
		}
		if rule.ID != strings.TrimSpace(rule.ID) {
			return nil, fmt.Errorf("%s: id must not have surrounding whitespace", prefix)
		}
		if _, exists := ids[rule.ID]; exists {
			return nil, fmt.Errorf("%s: duplicate id %q", prefix, rule.ID)
		}
		ids[rule.ID] = struct{}{}
		if strings.TrimSpace(rule.Name) == "" || len(rule.Name) > MaxNameLength {
			return nil, fmt.Errorf("%s: name must be nonempty and at most %d bytes", prefix, MaxNameLength)
		}
		if strings.TrimSpace(rule.Pattern) == "" || len(rule.Pattern) > MaxPatternLength {
			return nil, fmt.Errorf("%s: pattern must be nonempty and at most %d bytes", prefix, MaxPatternLength)
		}
		if len(rule.Message) > MaxMessageLength {
			return nil, fmt.Errorf("%s: message must be at most %d bytes", prefix, MaxMessageLength)
		}
		pattern, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): invalid regular expression: %w", prefix, rule.ID, err)
		}
		if rule.Enabled {
			policy.rules = append(policy.rules, compiledRule{rule: rule, pattern: pattern, matchGroup: pattern.SubexpIndex("match")})
		}
	}
	return policy, nil
}

// Check returns the first blocking rule, or nil when the call is allowed. Args
// should be JSON-compatible and must not be mutated while Check is running.
func (p *Policy) Check(toolName string, args map[string]interface{}) *Match {
	if p == nil || !p.config.Enabled || len(p.rules) == 0 {
		return nil
	}
	candidates := candidateTexts(toolName, args)
	for _, rule := range p.rules {
		for _, candidate := range candidates {
			indices := rule.pattern.FindStringSubmatchIndex(candidate)
			if indices == nil {
				continue
			}
			start, end := indices[0], indices[1]
			if group := rule.matchGroup; group > 0 && indices[group*2] >= 0 {
				start, end = indices[group*2], indices[group*2+1]
			}
			matched := candidate[start:end]
			message := rule.rule.Message
			if strings.TrimSpace(message) == "" {
				message = defaultMessage
			}
			// A single replacement pass prevents matched text from introducing
			// additional template substitutions.
			message = strings.NewReplacer("{match}", matched, "{tool}", toolName, "{rule}", rule.rule.Name).Replace(message)
			return &Match{RuleID: rule.rule.ID, RuleName: rule.rule.Name, MatchedText: matched, Message: message}
		}
	}
	return nil
}

// Manager atomically replaces validated policy snapshots for live settings.
type Manager struct {
	policy atomic.Pointer[Policy]
}

func NewManager(config Config) (*Manager, error) {
	m := &Manager{}
	if err := m.Update(config); err != nil {
		return nil, err
	}
	return m, nil
}

// Update retains the active policy if validation fails.
func (m *Manager) Update(config Config) error {
	policy, err := Compile(config)
	if err != nil {
		return err
	}
	m.policy.Store(policy)
	return nil
}

func (m *Manager) Config() Config {
	if m == nil {
		return Config{}
	}
	if policy := m.policy.Load(); policy != nil {
		return cloneConfig(policy.config)
	}
	return Config{}
}

func (m *Manager) Check(toolName string, args map[string]interface{}) *Match {
	if m == nil {
		return nil
	}
	return m.policy.Load().Check(toolName, args)
}

func cloneConfig(config Config) Config {
	if config.Rules != nil {
		rules := make([]Rule, len(config.Rules))
		copy(rules, config.Rules)
		config.Rules = rules
	}
	return config
}

func candidateTexts(toolName string, args map[string]interface{}) []string {
	var candidates []string
	seen := make(map[string]struct{})
	add := func(value string) {
		// Decode at most three rounds to cover common nested URL escaping
		// without claiming support for arbitrarily encoded tool inputs.
		for round := 0; round <= 3; round++ {
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				candidates = append(candidates, value)
			}
			decoded := decodePercentEscapes(value)
			if decoded == value {
				break
			}
			value = decoded
		}
	}
	add(toolName)
	var walk func(interface{}, int)
	walk = func(value interface{}, remainingDepth int) {
		if remainingDepth == 0 {
			return
		}
		switch value := value.(type) {
		case string:
			add(value)
		case map[string]interface{}:
			keys := make([]string, 0, len(value))
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				add(key)
				walk(value[key], remainingDepth-1)
			}
		case []interface{}:
			for _, item := range value {
				walk(item, remainingDepth-1)
			}
		}
	}
	// Unmarshaling also normalizes typed slices/maps, json.RawMessage and
	// escaped JSON keys/values into the recursive representation above.
	if raw, err := json.Marshal(args); err == nil {
		var value interface{}
		if json.Unmarshal(raw, &value) == nil {
			// encoding/json accepts at most 10,000 nesting levels. The decoded
			// tree is acyclic, so inspect every accepted string/key at that depth.
			walk(value, 10001)
		}
		add(string(raw))
	} else {
		// MCP rejects invalid JSON arguments independently; still inspect the
		// ordinary values if a caller supplies a non-JSON value alongside them.
		walk(args, 128)
	}
	return candidates
}

// Decode valid percent triplets even if another part of the string has a stray
// percent sign. Whole-string URL unescaping otherwise misses such mixed inputs.
func decodePercentEscapes(value string) string {
	if !strings.Contains(value, "%") {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '%' && i+2 < len(value) {
			hi, okHi := hexValue(value[i+1])
			lo, okLo := hexValue(value[i+2])
			if okHi && okLo {
				out.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func hexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}
