package security

import (
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/config"
	"go.uber.org/zap"
)

// Check the public tool's YAML through the same argument mapping used by MCP.
// In particular, an empty optional string is dropped by that mapping, so the
// status-policy off switch must use a nonempty sentinel.
func TestHTTPProxyPoolToolArguments(t *testing.T) {
	tool, err := config.LoadToolFromFile(filepath.Join("..", "..", "tools", "http-framework-test.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(&config.SecurityConfig{Tools: []config.ToolConfig{*tool}}, nil, zap.NewNop())
	pool := "http://user:p%40ss@localhost:8081\nhttp://localhost:8082"
	args := executor.buildCommandArgs(tool.Name, tool, map[string]interface{}{
		"url": "https://target.invalid/a/../b", "proxy_pool": pool,
		"proxy_source":       "https://provider.invalid/?key=placeholder",
		"proxy_max_attempts": float64(2), "proxy_cooldown": "0", "proxy_fail_status": "none",
	})
	want := map[string]string{
		"--proxy-pool": pool, "--proxy-source": "https://provider.invalid/?key=placeholder",
		"--proxy-max-attempts": "2", "--proxy-cooldown": "0", "--proxy-fail-status": "none",
	}
	for flag, value := range want {
		found := false
		for i, arg := range args {
			if arg == flag && i+1 < len(args) && args[i+1] == value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool argument mapping lost or changed %s", flag)
		}
	}
}
