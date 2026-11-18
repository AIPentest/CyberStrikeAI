package database

// Use the same definition for the coverage counts and for both asset filters.
const assessmentObservedAssetIDs = `SELECT j.value FROM deep_assessment_facts f, json_each(f.assets_json) j
	WHERE f.task_id=? AND f.confidence='observed' AND j.type='text'`

func (db *DB) AssessmentAssetCoverage(task, project string, access RBACListAccess) (map[string]int, error) {
	where, args := assetWhere(AssetListFilter{AssessmentID: task, ProjectID: project}, access)
	var total, observed int
	query := "SELECT COUNT(*), COALESCE(SUM(assets.id IN (" + assessmentObservedAssetIDs + ")),0) FROM assets" + where
	if err := db.QueryRow(query, append([]any{task}, args...)...).Scan(&total, &observed); err != nil {
		return nil, err
	}
	return map[string]int{"linked": total, "with_observed_evidence": observed}, nil
}

func (db *DB) CanReadAssessmentAsset(task, project, asset string, access RBACListAccess) (bool, error) {
	where, args := assetWhere(AssetListFilter{ID: asset, AssessmentID: task, ProjectID: project}, access)
	var allowed bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM assets"+where+")", args...).Scan(&allowed)
	return allowed, err
}

func (db *DB) ValidateIntentEvidence(task, step string, refs []string) error {
	_, err := assessmentReceipts(db, task, step, refs)
	return err
}
