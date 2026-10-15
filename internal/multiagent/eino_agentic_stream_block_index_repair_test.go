package multiagent

import (
	"context"
	"io"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// agenticBlockIndexFakeModel replays a fixed frame sequence, mimicking a
// provider stream that interleaves a whitespace text chunk between two
// parallel tool-call blocks (observed on Cloudflare Workers AI gateways).
type agenticBlockIndexFakeModel struct {
	frames []*schema.AgenticMessage
}

func (m *agenticBlockIndexFakeModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return schema.ConcatAgenticMessages(m.frames)
}

func (m *agenticBlockIndexFakeModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	sr, sw := schema.Pipe[*schema.AgenticMessage](len(m.frames) + 1)
	go func() {
		for _, f := range m.frames {
			sw.Send(f, nil)
		}
		sw.Close()
	}()
	return sr, nil
}

func streamBlock(idx int, block *schema.ContentBlock) *schema.AgenticMessage {
	block.StreamingMeta = &schema.StreamingMeta{Index: idx}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{block},
	}
}

func TestAgenticStreamBlockIndexRepair_InterleavedTextBetweenToolCalls(t *testing.T) {
	// Converter output for: text -> tool call 1 -> interleaved "\n" text -> tool call 2.
	// The interleaved text wrongly shares index 1 with tool call 1.
	frames := []*schema.AgenticMessage{
		streamBlock(0, schema.NewContentBlock(&schema.AssistantGenText{Text: "\n\n"})),
		streamBlock(1, schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call_1", Name: "exec", Arguments: ""})),
		streamBlock(1, schema.NewContentBlock(&schema.FunctionToolCall{Arguments: `{"command": "whoami"}`})),
		streamBlock(1, schema.NewContentBlock(&schema.AssistantGenText{Text: "\n"})),
		streamBlock(2, schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call_2", Name: "exec", Arguments: ""})),
		streamBlock(2, schema.NewContentBlock(&schema.FunctionToolCall{Arguments: `{"command": "id"}`})),
	}

	if _, err := schema.ConcatAgenticMessages(frames); err == nil {
		t.Fatal("precondition failed: broken frames should fail ConcatAgenticMessages")
	}

	repaired := newAgenticStreamBlockIndexRepairModel(&agenticBlockIndexFakeModel{frames: frames})
	sr, err := repaired.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got []*schema.AgenticMessage
	for {
		frame, rerr := sr.Recv()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			t.Fatalf("Recv: %v", rerr)
		}
		got = append(got, frame)
	}

	msg, err := schema.ConcatAgenticMessages(got)
	if err != nil {
		t.Fatalf("ConcatAgenticMessages after repair: %v", err)
	}
	if msg == nil {
		t.Fatal("aggregated message is nil after repair")
	}
	var toolCalls int
	var text string
	for _, b := range msg.ContentBlocks {
		if b.FunctionToolCall != nil {
			toolCalls++
		}
		if b.AssistantGenText != nil {
			text += b.AssistantGenText.Text
		}
	}
	if toolCalls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", toolCalls)
	}
	if text != "\n\n\n" {
		t.Fatalf("expected concatenated text %q, got %q", "\n\n\n", text)
	}
}

func TestAgenticStreamBlockIndexRepair_ValidStreamUntouched(t *testing.T) {
	frames := []*schema.AgenticMessage{
		streamBlock(0, schema.NewContentBlock(&schema.Reasoning{Text: "思考"})),
		streamBlock(1, schema.NewContentBlock(&schema.AssistantGenText{Text: "你好"})),
		streamBlock(1, schema.NewContentBlock(&schema.AssistantGenText{Text: "世界"})),
	}
	repaired := newAgenticStreamBlockIndexRepairModel(&agenticBlockIndexFakeModel{frames: frames})
	sr, err := repaired.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got []*schema.AgenticMessage
	for {
		frame, rerr := sr.Recv()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			t.Fatalf("Recv: %v", rerr)
		}
		got = append(got, frame)
	}
	for i, frame := range got {
		for j, b := range frame.ContentBlocks {
			if b.StreamingMeta == nil || frames[i].ContentBlocks[j].StreamingMeta == nil {
				continue
			}
			if b.StreamingMeta.Index != frames[i].ContentBlocks[j].StreamingMeta.Index {
				t.Fatalf("frame %d block %d index changed: %d -> %d", i, j,
					frames[i].ContentBlocks[j].StreamingMeta.Index, b.StreamingMeta.Index)
			}
		}
	}
}
