package config

// LearningConfig enables the experimental, draft-only post-task learner.
// Settings are captured at startup; changing them requires a restart.
type LearningConfig struct {
	Enabled        bool `yaml:"enabled" json:"enabled"`
	MaxJobsPerDay  int  `yaml:"max_jobs_per_day,omitempty" json:"max_jobs_per_day,omitempty"`
	TimeoutSeconds int  `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	MaxInputRunes  int  `yaml:"max_input_runes,omitempty" json:"max_input_runes,omitempty"`
}

func (c LearningConfig) DailyLimit() int { return boundedLearningValue(c.MaxJobsPerDay, 20, 200) }
func (c LearningConfig) Timeout() int    { return boundedLearningValue(c.TimeoutSeconds, 60, 120) }
func (c LearningConfig) InputLimit() int { return boundedLearningValue(c.MaxInputRunes, 12000, 32000) }
func boundedLearningValue(n, fallback, maximum int) int {
	if n <= 0 {
		return fallback
	}
	if n > maximum {
		return maximum
	}
	return n
}
