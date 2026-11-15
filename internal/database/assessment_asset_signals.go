package database

// Shared asset changes notify only assessments already referencing that asset.
// Timestamps, risk-cache writes and scan counters do not wake the planner.
func (db *DB) initAssessmentAssetSignals() error {
	queries := []string{
		`CREATE TRIGGER IF NOT EXISTS assessment_asset_changed AFTER UPDATE OF host,domain,ip,port,protocol,title,server,business_system,status ON assets
 WHEN OLD.host IS NOT NEW.host OR OLD.domain IS NOT NEW.domain OR OLD.ip IS NOT NEW.ip OR OLD.port IS NOT NEW.port OR OLD.protocol IS NOT NEW.protocol OR OLD.title IS NOT NEW.title OR OLD.server IS NOT NEW.server OR OLD.business_system IS NOT NEW.business_system OR OLD.status IS NOT NEW.status
 BEGIN INSERT INTO deep_assessment_changes(task_id,version) SELECT task_id,1 FROM deep_assessment_assets WHERE asset_id=NEW.id ON CONFLICT(task_id) DO UPDATE SET version=version+1; END`,
		`CREATE TRIGGER IF NOT EXISTS assessment_endpoint_added AFTER INSERT ON asset_endpoints
 BEGIN INSERT INTO deep_assessment_changes(task_id,version) SELECT task_id,1 FROM deep_assessment_assets WHERE asset_id=NEW.asset_id ON CONFLICT(task_id) DO UPDATE SET version=version+1; END`,
		`CREATE TRIGGER IF NOT EXISTS assessment_endpoint_changed AFTER UPDATE OF title ON asset_endpoints WHEN OLD.title IS NOT NEW.title
 BEGIN INSERT INTO deep_assessment_changes(task_id,version) SELECT task_id,1 FROM deep_assessment_assets WHERE asset_id=NEW.asset_id ON CONFLICT(task_id) DO UPDATE SET version=version+1; END`,
	}
	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
