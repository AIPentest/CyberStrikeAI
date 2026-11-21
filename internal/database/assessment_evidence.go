package database

import "database/sql"

// AssessmentFindingReview is retained for JSON compatibility with older clients.
type AssessmentFindingReview struct {
	ID        string `json:"id"`
	StepID    string `json:"step_id"`
	Verdict   string `json:"verdict"`
	Reason    string `json:"reason"`
	Evidence  []string `json:"evidence"`
	CreatedAt interface{} `json:"created_at"`
}

func (db *DB) IsAssessmentConversation(conversation string) (bool, error) {
	_ = conversation
	return false, nil
}

func (db *DB) ValidateAssessmentFinding(conversation string, refs []string) error {
	_ = conversation
	_ = refs
	return nil
}

func recordAssessmentReport(tx *sql.Tx, v *Vulnerability) error {
	_ = tx
	_ = v
	return nil
}

func (db *DB) AssessmentFindingReviews(id string) ([]AssessmentFindingReview, error) {
	_ = id
	return nil, nil
}
