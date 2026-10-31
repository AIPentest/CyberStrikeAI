package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ProxyConfig governs only the built-in HTTP test tool, never process-wide routing.
type ProxyConfig struct {
	Mode              string `yaml:"mode" json:"mode"`
	URL               string `yaml:"proxy_url" json:"proxy_url"`
	Pool              string `yaml:"proxy_pool" json:"proxy_pool"`
	Source            string `yaml:"proxy_source" json:"proxy_source"`
	Strategy          string `yaml:"strategy" json:"strategy"`
	MaxAttempts       int    `yaml:"max_attempts" json:"max_attempts"`
	Cooldown          int    `yaml:"cooldown" json:"cooldown"`
	MinSwitchInterval int    `yaml:"min_switch_interval" json:"min_switch_interval"`
}

func (p ProxyConfig) Validate() error {
	if p.Mode != "" && p.Mode != "manual" && p.Mode != "fixed" && p.Mode != "pool" {
		return fmt.Errorf("proxy.mode must be manual, fixed or pool")
	}
	if p.Strategy != "" && p.Strategy != "sticky" && p.Strategy != "round_robin" {
		return fmt.Errorf("proxy.strategy must be sticky or round_robin")
	}
	if p.MaxAttempts < 0 || p.MaxAttempts > 10 || p.Cooldown < 0 || p.Cooldown > 86400 || p.MinSwitchInterval < 0 || p.MinSwitchInterval > 86400 {
		return fmt.Errorf("invalid proxy attempt or timing limits")
	}
	if p.Mode == "fixed" {
		return validateProxyEndpoint(p.URL)
	}
	if p.Mode != "pool" {
		return nil
	}
	count := 0
	for _, line := range strings.Split(p.Pool, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
			if err := validateProxyEndpoint(line); err != nil {
				return err
			}
		}
	}
	if count > 256 {
		return fmt.Errorf("proxy pool exceeds 256 endpoints")
	}
	if p.Source != "" {
		u, err := url.Parse(p.Source)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
			return fmt.Errorf("proxy source must be an HTTP(S) URL")
		}
	}
	if count == 0 && p.Source == "" {
		return fmt.Errorf("proxy pool requires endpoints or a provider URL")
	}
	return nil
}
func validateProxyEndpoint(value string) error {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "socks5") && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(value, "\r\n\t ") {
		if port, err := strconv.Atoi(u.Port()); err == nil && port > 0 && port <= 65535 {
			return nil
		}
	}
	return fmt.Errorf("proxy endpoint must be http(s)://host:port or socks5://host:port")
}
