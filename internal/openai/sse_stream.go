package openai

// SSEAccumulatedKey The server-authoritative streaming full-text snapshot field in the data of an SSE progress event.
// The frontend should prefer this field to update the buffer, to avoid duplicated characters from normalizing the delta twice.
const SSEAccumulatedKey = "accumulated"

// WithSSEAccumulated Attaches the current streaming cumulative full text to the progress data (the authoritative snapshot).
func WithSSEAccumulated(data map[string]interface{}, accumulated string) map[string]interface{} {
	if data == nil {
		data = make(map[string]interface{}, 1)
	}
	data[SSEAccumulatedKey] = accumulated
	return data
}

// NormalizeStreamingDelta will likely be"cumulative fragment / resent fragment"normalizes the content into"pure increment".
// Same as the unexported normalizeStreamingDelta; lets packages such as agent / multiagent accumulate the body before emitting SSE.
func NormalizeStreamingDelta(current, incoming string) (next, delta string) {
	return normalizeStreamingDelta(current, incoming)
}
