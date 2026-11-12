package config

import (
	"maps"
	"os"
	"slices"
	"strings"
)

// expandEnvVar 展开字符串中的 ${VAR} 和 ${VAR:-default} 环境变量引用。
// 与官方 MCP 配置格式一致（Claude Desktop / Cursor / VS Code 均支持此语法）。
func expandEnvVar(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		// 查找 ${
		idx := strings.Index(s[i:], "${")
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+idx])
		i += idx + 2 // skip ${

		// 查找对应的 }
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			// 没有 }，原样保留
			b.WriteString("${")
			continue
		}
		expr := s[i : i+end]
		i += end + 1 // skip }

		// 解析 VAR:-default
		varName := expr
		defaultVal := ""
		hasDefault := false
		if colonIdx := strings.Index(expr, ":-"); colonIdx >= 0 {
			varName = expr[:colonIdx]
			defaultVal = expr[colonIdx+2:]
			hasDefault = true
		}

		val := os.Getenv(varName)
		if val == "" && hasDefault {
			val = defaultVal
		}
		b.WriteString(val)
	}
	return b.String()
}

// ExpandConfigEnv 展开 ExternalMCPServerConfig 中所有支持环境变量的字段。
// 展开范围：Command、Args、Env values、URL、Headers values。
func ExpandConfigEnv(cfg *ExternalMCPServerConfig) {
	// Config structs are passed by value to background MCP connections. Their
	// slices and maps still share storage; expansion must not mutate that storage.
	cfg.Args = slices.Clone(cfg.Args)
	cfg.Env = maps.Clone(cfg.Env)
	cfg.Headers = maps.Clone(cfg.Headers)
	cfg.Command = expandEnvVar(cfg.Command)
	for i, arg := range cfg.Args {
		cfg.Args[i] = expandEnvVar(arg)
	}
	for k, v := range cfg.Env {
		cfg.Env[k] = expandEnvVar(v)
	}
	cfg.URL = expandEnvVar(cfg.URL)
	for k, v := range cfg.Headers {
		cfg.Headers[k] = expandEnvVar(v)
	}
}

// ExpandHeaderMap 返回 headers 的副本，并展开其中的 ${VAR} / ${VAR:-default} 引用。
// 与 MCP 配置的 Headers 展开规则保持一致。
func ExpandHeaderMap(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[expandEnvVar(k)] = expandEnvVar(v)
	}
	return out
}
