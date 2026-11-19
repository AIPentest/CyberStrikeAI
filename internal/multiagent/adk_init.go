package multiagent

import (
	"fmt"

	"github.com/cloudwego/eino/adk"
)

// InitADK configures global Eino ADK settings. Call once at process startup before
// any ADK middleware or agents are created.
//
// SetLanguage is process-global (not per-request), so we default to English for
// the localized build; the Eino skill middleware then emits English prompts and
// tool results ("Launching skill: …", "Base directory for this skill: …") instead
// of the Chinese variants ("正在启动 Skill：…", "此 Skill 的目录：…").
func InitADK() error {
	if err := adk.SetLanguage(adk.LanguageEnglish); err != nil {
		return fmt.Errorf("adk set language: %w", err)
	}
	return nil
}
