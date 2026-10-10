package multiagent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// agenticStreamBlockIndexRepairModel repairs StreamingMeta index collisions on
// the agentic streaming path. Some providers (observed: Cloudflare Workers AI
// gateways) interleave whitespace text chunks between parallel tool-call
// chunks in one SSE stream. The chunk converter in eino-ext acl/openai then
// assigns the text chunk the same StreamingMeta.Index as the preceding
// tool-call block, because it only tracks content-type transitions and does
// not account for tool calls having started. schema.ConcatAgenticMessages
// groups blocks by that index and fails with "content block type mismatch";
// the ADK retry checker swallows that error and sees a nil message, which
// surfaces as a misleading empty-model-output rejection and an endless
// deterministic retry loop.
//
// The wrapper keeps valid streams untouched. When a block's index was already
// used by a block of a different kind (tool call vs. text/reasoning), the
// block is moved to a fresh index so every concat group stays single-typed.
type agenticStreamBlockIndexRepairModel struct {
	base model.AgenticModel
}

func newAgenticStreamBlockIndexRepairModel(base model.AgenticModel) model.AgenticModel {
	if base == nil {
		return nil
	}
	return &agenticStreamBlockIndexRepairModel{base: base}
}

func (m *agenticStreamBlockIndexRepairModel) Generate(
	ctx context.Context,
	input []*schema.AgenticMessage,
	opts ...model.Option,
) (*schema.AgenticMessage, error) {
	return m.base.Generate(ctx, input, opts...)
}

func (m *agenticStreamBlockIndexRepairModel) Stream(
	ctx context.Context,
	input []*schema.AgenticMessage,
	opts ...model.Option,
) (*schema.StreamReader[*schema.AgenticMessage], error) {
	stream, err := m.base.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	state := newAgenticBlockIndexRepairState()
	return schema.StreamReaderWithConvert(stream, state.repairMessage), nil
}

type agenticBlockIndexRepairState struct {
	kindByIndex map[int]string // first-seen block kind per original converter index
	reassigned  map[string]int // "idx|kind" of colliding blocks -> reassigned index
	nextFree    int            // reassigned indices count down from -1 so they can
	// never collide with converter-assigned indices (>= 0) that appear later in
	// the stream. All chunks of the same colliding (idx, kind) pair share one
	// reassigned index, so they still merge into a single block. Reassigned
	// blocks sort before index 0 in the final message; block content is fully
	// preserved and only the position of stray interleaved text moves.
}

func newAgenticBlockIndexRepairState() *agenticBlockIndexRepairState {
	return &agenticBlockIndexRepairState{
		kindByIndex: make(map[int]string),
		reassigned:  make(map[string]int),
		nextFree:    -1,
	}
}

func agenticBlockKind(block *schema.ContentBlock) string {
	if block != nil && block.FunctionToolCall != nil {
		return "tool"
	}
	return "content"
}

func (s *agenticBlockIndexRepairState) repairMessage(msg *schema.AgenticMessage) (*schema.AgenticMessage, error) {
	if msg == nil || len(msg.ContentBlocks) == 0 {
		return msg, nil
	}
	var out *schema.AgenticMessage
	for i, block := range msg.ContentBlocks {
		if block == nil || block.StreamingMeta == nil {
			continue
		}
		idx := block.StreamingMeta.Index
		kind := agenticBlockKind(block)
		prev, seen := s.kindByIndex[idx]
		if !seen {
			s.kindByIndex[idx] = kind
			continue
		}
		if prev == kind {
			continue
		}
		// Index collision across block kinds: reassign a fresh (negative) index
		// so ConcatAgenticMessages never groups mismatched block types.
		key := fmt.Sprintf("%d|%s", idx, kind)
		newIdx, ok := s.reassigned[key]
		if !ok {
			newIdx = s.nextFree
			s.nextFree--
			s.reassigned[key] = newIdx
		}
		if out == nil {
			cp := *msg
			cp.ContentBlocks = append([]*schema.ContentBlock(nil), msg.ContentBlocks...)
			out = &cp
		}
		newBlock := *block
		newMeta := *block.StreamingMeta
		newMeta.Index = newIdx
		newBlock.StreamingMeta = &newMeta
		out.ContentBlocks[i] = &newBlock
	}
	if out != nil {
		return out, nil
	}
	return msg, nil
}
