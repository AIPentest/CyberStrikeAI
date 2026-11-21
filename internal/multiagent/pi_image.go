package multiagent

import (
	"context"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"

	"github.com/google/uuid"
)

// ProbeImageInput sends a synthetic image through the channel's Pi adapter.
func ProbeImageInput(ctx context.Context, runtime config.AgentRuntimeConfig, oa config.OpenAIConfig, img agentruntime.Image) (*agentruntime.Result, error) {
	oa.ImageInput = "enabled"
	oa.Reasoning.Mode = "off"
	oa.MaxCompletionTokens = 128
	model := piModelConfig(oa, nil, "image-test")
	req := agentruntime.Request{
		RunID: uuid.NewString(), GenerateOnly: true, Model: model,
		Message: "Read the six digits printed in the supplied image. Reply with only those digits. If there is no image, reply NO_IMAGE.",
		Images:  []agentruntime.Image{img}, Tools: []agentruntime.Tool{}, MaxTurns: 1,
	}
	return (agentruntime.Pi{NodePath: runtime.NodePath, WorkerPath: runtime.WorkerPath}).Run(ctx, req, nil, nil, nil)
}
