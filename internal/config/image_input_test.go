package config

import "testing"

func TestImageInputCapability(t *testing.T) {
	tests := []struct {
		url, model, mode string
		want             string
	}{
		{"https://api.deepseek.com", "deepseek-flash", "", "enabled"},
		{"https://api.deepseek.com/v1", "deepseek-v4-pro", "", "disabled"},
		{"https://api.deepseek.com", "deepseek-flash", "disabled", "disabled"},
		{"https://custom.example/v1", "deepseek-flash", "", "unknown"},
		{"https://custom.example/v1", "local-model", "enabled", "enabled"},
	}
	for _, tt := range tests {
		capability := (OpenAIConfig{BaseURL: tt.url, Model: tt.model, ImageInput: tt.mode}).ImageInputCapability()
		got := "unknown"
		if capability != nil {
			got = "disabled"
			if *capability {
				got = "enabled"
			}
		}
		if got != tt.want {
			t.Errorf("%s %s %s: got %s, want %s", tt.url, tt.model, tt.mode, got, tt.want)
		}
	}
}
