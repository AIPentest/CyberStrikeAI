package security

import (
	"encoding/json"
	"os/exec"
	"strings"

	"cyberstrike-ai/internal/config"
)

func (e *Executor) SetProxyConfig(cfg config.ProxyConfig) {
	e.proxyMu.Lock()
	defer e.proxyMu.Unlock()
	e.proxyConfig = cfg
}
func (e *Executor) proxySnapshot(tool string) string {
	if tool != "http-framework-test" {
		return ""
	}
	e.proxyMu.RLock()
	defer e.proxyMu.RUnlock()
	if e.proxyConfig.Mode == "" || e.proxyConfig.Mode == "manual" {
		return ""
	}
	policy := e.proxyConfig
	if policy.Mode == "fixed" {
		policy.Pool, policy.Source = "", ""
	} else {
		policy.URL = ""
	}
	raw, _ := json.Marshal(policy)
	return string(raw)
}
func applyHTTPProxySnapshot(cmd *exec.Cmd, snapshot string) {
	// Never mutate os.Environ or HTTP_PROXY: models and other tools are isolated.
	env := cmd.Environ()
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "CSAI_HTTP_PROXY_CONFIG=") {
			filtered = append(filtered, entry)
		}
	}
	if snapshot != "" {
		filtered = append(filtered, "CSAI_HTTP_PROXY_CONFIG="+snapshot)
	}
	cmd.Env = filtered
}
