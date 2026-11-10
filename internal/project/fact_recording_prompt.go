package project

import "cyberstrike-ai/internal/projectprompt"

// FactRecordingIncrementalRhythmMarkdown see projectprompt.
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	return projectprompt.FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent)
}

// FactRecordingBlackboardSection see projectprompt.
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	return projectprompt.FactRecordingBlackboardSection(coordinatorDelegate)
}

// FactRecordingSubAgentSection see projectprompt.
func FactRecordingSubAgentSection() string {
	return projectprompt.FactRecordingSubAgentSection()
}

// FactRecordingBlackboardSectionMarkdown see projectprompt.
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	return projectprompt.FactRecordingBlackboardSectionMarkdown(coordinatorDelegate)
}
