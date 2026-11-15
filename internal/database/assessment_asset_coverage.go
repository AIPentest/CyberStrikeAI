package database

func (db *DB) AssessmentAssetCoverage(task, project string, access RBACListAccess) (map[string]int, error) {
	where, args := assetWhere(AssetListFilter{AssessmentID: task, ProjectID: project}, access)
	var total, observed int
	if err := db.QueryRow("SELECT COUNT(*) FROM assets"+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	where += " AND EXISTS(SELECT 1 FROM deep_assessment_facts f,json_each(f.assets_json) j WHERE f.task_id=? AND f.confidence='observed' AND j.value=assets.id)"
	args = append(args, task)
	if err := db.QueryRow("SELECT COUNT(*) FROM assets"+where, args...).Scan(&observed); err != nil {
		return nil, err
	}
	return map[string]int{"linked": total, "with_observed_evidence": observed}, nil
}
func (db *DB) ValidateIntentEvidence(task, step string, refs []string) error {
	_, err := assessmentReceipts(db, task, step, refs)
	return err
}
