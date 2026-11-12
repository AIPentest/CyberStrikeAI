package multiagent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// agenticStreamIndexRepairModel isolates converter index collisions before
// ADK's retry checker concatenates the stream. Some OpenAI-compatible providers
// interleave text and tool-call deltas; eino-ext can assign both block types the
// same StreamingMeta.Index. Concat then fails and ADK interprets the lost output
// as an empty response. See cloudwego/eino-ext#975.
type agenticStreamIndexRepairModel struct {
	base model.AgenticModel
}

func newAgenticStreamIndexRepairModel(base model.AgenticModel) model.AgenticModel {
	if base == nil {
		return nil
	}
	return &agenticStreamIndexRepairModel{base: base}
}

func (m *agenticStreamIndexRepairModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return m.base.Generate(ctx, input, opts...)
}

func (m *agenticStreamIndexRepairModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	stream, err := m.base.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	state := agenticStreamIndexRepairState{
		indices:  make(map[agenticStreamBlockKey]int),
		used:     make(map[int]struct{}),
		maxIndex: -1,
	}
	return schema.StreamReaderWithConvert(stream, state.repairMessage), nil
}

type agenticStreamBlockKey struct {
	index int
	kind  schema.ContentBlockType
}

type agenticStreamIndexRepairState struct {
	indices  map[agenticStreamBlockKey]int
	used     map[int]struct{}
	maxIndex int
}

func (s *agenticStreamIndexRepairState) repairMessage(msg *schema.AgenticMessage) (*schema.AgenticMessage, error) {
	if msg == nil {
		return msg, nil
	}
	var out *schema.AgenticMessage
	for i, block := range msg.ContentBlocks {
		if block == nil || block.StreamingMeta == nil {
			continue
		}
		key := agenticStreamBlockKey{index: block.StreamingMeta.Index, kind: block.Type}
		index, seen := s.indices[key]
		if !seen {
			index = key.index
			if _, occupied := s.used[index]; occupied {
				// Allocate after existing blocks so interleaved content keeps its
				// order. Later source indices cannot collide with these repairs.
				index = s.maxIndex + 1
			}
			s.indices[key] = index
			s.used[index] = struct{}{}
			if index > s.maxIndex {
				s.maxIndex = index
			}
		}
		if index == key.index {
			continue
		}
		if out == nil {
			cp := *msg
			cp.ContentBlocks = append([]*schema.ContentBlock(nil), msg.ContentBlocks...)
			out = &cp
		}
		// Streams can be copied by Eino for callbacks and retry inspection.
		// Never mutate the provider's message, block, or metadata in place.
		cp := *block
		meta := *block.StreamingMeta
		meta.Index = index
		cp.StreamingMeta = &meta
		out.ContentBlocks[i] = &cp
	}
	if out != nil {
		return out, nil
	}
	return msg, nil
}
