package database

import "encoding/json"

// AssessmentEvidenceAssets resolves the readable anchors for an evidence projection.
func (db *DB) AssessmentEvidenceAssets(task, project string, ids []string, access RBACListAccess) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 || access.UserID == "" {
		return out, nil
	}
	where, args := assetWhere(AssetListFilter{AssessmentID: task, ProjectID: project}, access)
	raw, _ := json.Marshal(ids)
	rows, err := db.Query(`SELECT assets.id,COALESCE(NULLIF(assets.host,''),NULLIF(assets.domain,''),NULLIF(assets.ip,''),assets.id)
		FROM assets`+where+` AND assets.id IN (SELECT value FROM json_each(?))`, append(args, string(raw))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[id] = title
	}
	return out, rows.Err()
}
