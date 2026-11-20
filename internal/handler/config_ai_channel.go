package handler

import (
	"fmt"
	"maps"
	"strings"

	"cyberstrike-ai/internal/config"
)

// AIChannelUpdate edits an existing channel without replacing the channel map.
// An empty ID selects the legacy openai configuration when there are no channels.
type AIChannelUpdate struct {
	ChannelID           string  `json:"channel_id"`
	MakeDefault         bool    `json:"make_default,omitempty"`
	Name                *string `json:"name,omitempty"`
	Provider            *string `json:"provider,omitempty"`
	BaseURL             *string `json:"base_url,omitempty"`
	APIKey              *string `json:"api_key,omitempty"`
	Model               *string `json:"model,omitempty"`
	ImageInput          *string `json:"image_input,omitempty"`
	ContextProfile      *string `json:"context_profile,omitempty"`
	ContextWindow       *int    `json:"context_window,omitempty"`
	MaxTotalTokens      *int    `json:"max_total_tokens,omitempty"`
	MaxCompletionTokens *int    `json:"max_completion_tokens,omitempty"`
}

func (u AIChannelUpdate) merge(cfg *config.Config) (*config.AIConfig, error) {
	ai := cfg.AI
	ai.Channels = maps.Clone(ai.Channels)
	id := strings.TrimSpace(u.ChannelID)
	if id == "" && len(ai.Channels) == 0 {
		ai.EnsureDefaultFromOpenAI(cfg.OpenAI)
		id = ai.DefaultChannel
	}
	ch, ok := ai.Channels[id]
	if !ok || id == "" {
		return nil, fmt.Errorf("通道已不存在，请刷新配置后重试")
	}
	if u.ImageInput != nil {
		switch strings.TrimSpace(*u.ImageInput) {
		case "", "auto":
			ch.ImageInput = ""
		case "enabled", "disabled":
			ch.ImageInput = strings.TrimSpace(*u.ImageInput)
		default:
			return nil, fmt.Errorf("图片输入模式无效")
		}
	} else if (u.Model != nil && strings.TrimSpace(*u.Model) != ch.Model) ||
		(u.BaseURL != nil && strings.TrimSpace(*u.BaseURL) != ch.BaseURL) ||
		(u.Provider != nil && strings.TrimSpace(*u.Provider) != ch.Provider) {
		ch.ImageInput = ""
	}

	for _, field := range []struct{ target, value *string }{
		{&ch.Name, u.Name},
		{&ch.Provider, u.Provider},
		{&ch.BaseURL, u.BaseURL},
		{&ch.APIKey, u.APIKey},
		{&ch.Model, u.Model},
		{&ch.ContextProfile, u.ContextProfile},
	} {
		if field.value != nil {
			*field.target = strings.TrimSpace(*field.value)
		}
	}
	for _, field := range []struct {
		name          string
		target, value *int
	}{
		{"context_window", &ch.ContextWindow, u.ContextWindow},
		{"max_total_tokens", &ch.MaxTotalTokens, u.MaxTotalTokens},
		{"max_completion_tokens", &ch.MaxCompletionTokens, u.MaxCompletionTokens},
	} {
		if field.value != nil {
			if *field.value < 0 {
				return nil, fmt.Errorf("%s 不能为负数", field.name)
			}
			*field.target = *field.value
		}
	}
	if u.MakeDefault {
		ai.DefaultChannel = id
	}
	ai.Channels[id] = ch
	return &ai, nil
}
