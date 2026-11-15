package handler

import (
	"cyberstrike-ai/internal/deepassessment"
	"fmt"
)

func assessmentObject(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": append([]string{}, required...), "additionalProperties": false}
}
func assessmentString() map[string]any { return map[string]any{"type": "string"} }
func assessmentStrings() map[string]any {
	return map[string]any{"type": "array", "items": assessmentString()}
}
func assessmentPlanSchema() map[string]any {
	return assessmentObject(map[string]any{"strategy": assessmentString(), "summary": assessmentString(), "question": assessmentString(), "retire": assessmentStrings(), "steps": map[string]any{"type": "array", "maxItems": 8, "items": assessmentObject(map[string]any{"id": assessmentString(), "title": assessmentString(), "instruction": assessmentString(), "depends_on": assessmentStrings(), "source_ids": assessmentStrings(), "asset_ids": assessmentStrings(), "priority": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}}, "id", "title", "instruction", "depends_on")}}, "strategy", "summary", "steps", "retire", "question")
}
func assessmentReviewSchema() map[string]any {
	return assessmentObject(map[string]any{"complete": map[string]any{"type": "boolean"}, "summary": assessmentString(), "missing": assessmentString(), "evidence": assessmentStrings()}, "complete", "summary", "missing", "evidence")
}
func assessmentMemorySchema() map[string]any {
	return assessmentObject(map[string]any{"query": assessmentString(), "step_id": assessmentString(), "offset": map[string]any{"type": "integer", "minimum": 0}, "step_offset": map[string]any{"type": "integer", "minimum": 0}}, "query")
}
func assessmentTraceSchema() map[string]any {
	return assessmentObject(map[string]any{"trace_id": assessmentString(), "offset": map[string]any{"type": "integer", "minimum": 0}}, "trace_id")
}
func assessmentRuntimeError(err error) error {
	if transientDeepAssessmentModelError(err) {
		return fmt.Errorf("%w: %v", deepassessment.ErrRetryable, err)
	}
	return err
}
