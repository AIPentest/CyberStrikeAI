package config

import (
	"net/url"
	"strings"
)

// ImageInputCapability resolves explicit channel settings and known official
// models whose capabilities are newer than the bundled Pi model catalog.
// A nil result lets Pi look up the model and endpoint in its catalog.
func (c OpenAIConfig) ImageInputCapability() *bool {
	switch c.ImageInput {
	case "enabled":
		enabled := true
		return &enabled
	case "disabled":
		enabled := false
		return &enabled
	}
	endpoint, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host != "api.deepseek.com" || endpoint.User != nil {
		return nil
	}
	switch strings.TrimRight(endpoint.Path, "/") {
	case "", "/v1":
	default:
		return nil
	}
	switch strings.TrimSpace(c.Model) {
	case "deepseek-flash", "deepseek-v4-flash-vision-exp":
		enabled := true
		return &enabled
	case "deepseek-v4-pro":
		enabled := false
		return &enabled
	default:
		return nil
	}
}
