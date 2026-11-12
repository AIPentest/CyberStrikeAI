package multiagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/reasoning"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type reasoningWireTransport func(*http.Request) (*http.Response, error)

func (f reasoningWireTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Exercise both production factories, real SDK serialization, planner setup,
// and the full transport chain. No requests leave the recording transport.
func TestReasoningProviderWireCompatibility(t *testing.T) {
	for _, kind := range []string{"classic", "agentic"} {
		for _, streaming := range []bool{false, true} {
			for _, tc := range []struct {
				name, url, model, profile, mode, effort string
				client                                  *reasoning.ClientIntent
				planner                                 bool
				wantEffort, wantThinking, wantErr       string
				stripChoice                             bool
			}{
				{name: "openai_off", model: "gpt-5.6-sol", mode: "off", effort: "high", wantEffort: "none"},
				{name: "openai_client_off", model: "gpt-5.6-sol", mode: "on", effort: "high", client: &reasoning.ClientIntent{Mode: "off"}, wantEffort: "none"},
				{name: "openai_explicit_none", model: "gpt-5.6-sol", mode: "on", effort: "none", wantEffort: "none"},
				{name: "openai_planner", model: "gpt-5.6-sol", mode: "on", effort: "high", planner: true, wantEffort: "none"},
				{name: "openai_55_off", model: "gpt-5.5", mode: "off", wantEffort: "none"},
				{name: "openai_6_sol_off", model: "gpt-6-sol", mode: "off", wantEffort: "none"},
				{name: "openai_high_tools", model: "gpt-5.6-sol", mode: "on", effort: "high", wantErr: "set reasoning mode to off"},
				{name: "openai_default_tools", model: "gpt-5.6-sol", mode: "auto", wantErr: "set reasoning mode to off"},
				{name: "openai_61_tools", model: "gpt-6.1-sol", mode: "off", wantErr: "requires /v1/responses"},
				{name: "openai_astra_planner", model: "gpt-6-astra", planner: true, wantErr: "requires /v1/responses"},
				{name: "older_openai_off", model: "gpt-4o-mini", mode: "off"},
				{name: "unknown_model_off", model: "gpt-6.2-sol", mode: "off"},
				{name: "deepseek_off", url: "https://api.deepseek.com/v1", model: "deepseek-v4-flash", mode: "off", wantThinking: "disabled"},
				{name: "deepseek_on", url: "https://api.deepseek.com/v1", model: "deepseek-v4-flash", mode: "on", effort: "high", wantEffort: "high", wantThinking: "enabled", stripChoice: true},
				{name: "deepseek_planner", url: "https://api.deepseek.com/v1", model: "deepseek-v4-flash", mode: "on", planner: true, wantThinking: "disabled"},
				{name: "explicit_deepseek_profile", url: "https://gateway.example/v1", model: "gpt-5.6-sol", profile: "deepseek_compat", mode: "off", wantThinking: "disabled"},
				{name: "gateway_same_model_off", url: "https://gateway.example/v1", model: "gpt-5.6-sol", mode: "off"},
				{name: "gateway_same_model_forced_on", url: "https://gateway.example/v1", model: "gpt-5.6-sol", mode: "on", effort: "high"},
				{name: "gateway_planner", url: "https://gateway.example/v1", model: "gpt-5.6-sol", mode: "on", planner: true},
				{name: "openai_profile_on_deepseek", url: "https://api.deepseek.com/v1", model: "deepseek-v4-flash", profile: "openai_compat", mode: "off"},
			} {
				name := kind + "/" + tc.name
				if streaming {
					name += "/stream"
				}
				t.Run(name, func(t *testing.T) {
					baseURL := tc.url
					if baseURL == "" {
						baseURL = "https://api.openai.com/v1"
					}
					oa := config.OpenAIConfig{Provider: "openai_compatible", APIKey: "local-test", BaseURL: baseURL, Model: tc.model,
						Reasoning: config.OpenAIReasoningConfig{Mode: tc.mode, Effort: tc.effort, Profile: tc.profile,
							ExtraRequestFields: map[string]interface{}{"vendor_option": true}}}
					var body map[string]any
					calls := 0
					client := &http.Client{Transport: reasoningWireTransport(func(req *http.Request) (*http.Response, error) {
						calls++
						if req.URL.Path != "/v1/chat/completions" {
							t.Errorf("unexpected endpoint: %s", req.URL.Path)
						}
						if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						content := `{"id":"test","object":"chat.completion","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
						contentType := "application/json"
						if streaming {
							contentType = "text/event-stream"
							content = "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(content)), Request: req}, nil
					})}
					ctx := context.Background()
					mode := einoModelModeNormal
					if tc.planner {
						mode = einoModelModePlanner
					}
					var chat model.ToolCallingChatModel
					var err error
					if kind == "classic" {
						chat, err = newEinoToolCallingChatModelFactory(client, tc.client, nil)(ctx, oa, mode)
					} else {
						var native model.AgenticModel
						native, err = newEinoAgenticChatModelFactory(client, tc.client, nil)(ctx, oa, mode)
						chat = newAgenticToolCallingChatModelAdapter(native)
					}
					if err != nil {
						t.Fatal(err)
					}
					opts := []model.Option{model.WithTools([]*schema.ToolInfo{{Name: "lookup", Desc: "Look up a value", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}}), model.WithToolChoice(schema.ToolChoiceForced)}
					input := []*schema.Message{schema.UserMessage("hello")}
					if streaming {
						var stream *schema.StreamReader[*schema.Message]
						stream, err = chat.Stream(ctx, input, opts...)
						if err == nil {
							defer stream.Close()
							for {
								_, recvErr := stream.Recv()
								if recvErr == io.EOF {
									break
								}
								if recvErr != nil {
									err = recvErr
									break
								}
							}
						}
					} else {
						_, err = chat.Generate(ctx, input, opts...)
					}
					if tc.wantErr != "" {
						if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
							t.Fatalf("error=%v, want %q", err, tc.wantErr)
						}
						if calls != 0 {
							t.Fatalf("invalid combination sent %d requests", calls)
						}
						if isEinoTransientRunError(err) {
							t.Fatalf("configuration error considered transient: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if calls != 1 {
						t.Fatalf("calls=%d, want 1", calls)
					}
					gotEffort, _ := body["reasoning_effort"].(string)
					if gotEffort != tc.wantEffort {
						t.Fatalf("effort=%q, want %q; body=%#v", gotEffort, tc.wantEffort, body)
					}
					thinking, _ := body["thinking"].(map[string]any)
					gotThinking, _ := thinking["type"].(string)
					if gotThinking != tc.wantThinking {
						t.Fatalf("thinking=%q, want %q", gotThinking, tc.wantThinking)
					}
					_, choice := body["tool_choice"]
					if choice == tc.stripChoice {
						t.Fatalf("tool_choice present=%v, strip=%v", choice, tc.stripChoice)
					}
					tools, _ := body["tools"].([]any)
					if len(tools) != 1 || body["vendor_option"] != true {
						t.Fatalf("lost tools or unrelated fields: %#v", body)
					}
				})
			}
		}
	}
}
