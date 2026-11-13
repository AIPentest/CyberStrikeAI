package config

import "strings"

// MainWebUIUsesHTTPS 判断主 Web UI 是否以 HTTPS 监听（与 internal/app.prepareMainServerTLS 前置条件一致）。
func MainWebUIUsesHTTPS(s *ServerConfig) bool {
	if s == nil {
		return false
	}
	if s.TLSEnabled {
		return true
	}
	if s.TLSAutoSelfSign {
		return true
	}
	cert := strings.TrimSpace(s.TLSCertPath)
	key := strings.TrimSpace(s.TLSKeyPath)
	return cert != "" && key != ""
}

// ServerHTTPRedirectEnabled 是否在主站启用 HTTPS 时把明文 HTTP 请求重定向到 HTTPS（默认开启）。
func ServerHTTPRedirectEnabled(s *ServerConfig) bool {
	if s == nil || !MainWebUIUsesHTTPS(s) {
		return false
	}
	if s.TLSHTTPRedirect == nil {
		return true
	}
	return *s.TLSHTTPRedirect
}

// ApplyDevHTTPSBootstrap 供 --https / 一键脚本使用：强制开启主站 TLS。
// 若已配置 tls_cert_path 与 tls_key_path 则使用 PEM，否则启用 tls_auto_self_sign（仅本地测试）。
func ApplyDevHTTPSBootstrap(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Server.TLSEnabled = true
	cert := strings.TrimSpace(cfg.Server.TLSCertPath)
	key := strings.TrimSpace(cfg.Server.TLSKeyPath)
	if cert != "" && key != "" {
		cfg.Server.TLSAutoSelfSign = false
		return
	}
	cfg.Server.TLSAutoSelfSign = true
}

// ApplyPlainHTTPBootstrap 供 --http / 一键脚本使用：强制主站使用明文 HTTP。
// 它会覆盖配置文件中的 TLS 开关、自签证书以及证书路径，避免 --http 仍被配置中的 HTTPS 选项重新启用。
func ApplyPlainHTTPBootstrap(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Server.TLSEnabled = false
	cfg.Server.TLSAutoSelfSign = false
	cfg.Server.TLSCertPath = ""
	cfg.Server.TLSKeyPath = ""
	disabled := false
	cfg.Server.TLSHTTPRedirect = &disabled
}
