package einomcp

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/agent"
)

func TestToolInfoFromDefinitionSanitizesOpenAIToolName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "fs.read", want: "fs_read"},
		{name: "nezha::server.exec", want: "nezha__server_exec"},
		{name: "already-valid_1", want: "already-valid_1"},
		{name: "space/slash", want: "space_slash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := toolInfoFromDefinition(agent.Tool{
				Type: "function",
				Function: agent.FunctionDefinition{
					Name:       tt.name,
					Parameters: map[string]interface{}{"type": "object"},
				},
			})
			if err != nil {
				t.Fatalf("toolInfoFromDefinition() error = %v", err)
			}
			if info.Name != tt.want {
				t.Fatalf("toolInfoFromDefinition() name = %q, want %q", info.Name, tt.want)
			}
		})
	}
}

func TestUnknownToolReminderText(t *testing.T) {
	s := unknownToolReminderText("bad_tool")
	if !strings.Contains(s, "bad_tool") {
		t.Fatalf("expected requested name in message: %s", s)
	}
	if strings.Contains(s, "Tools currently available") {
		t.Fatal("unified message must not list tool names")
	}
}
