package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AssetFingerprint 记录某个资产上探测到的服务/技术指纹与版本。
//
// 设计上刻意独立成表（而不是往 assets 表加列）：
//   - assets 表已有 20+ 处 SELECT/scan 代码，加列需要同步修改所有查询，回归风险高；
//   - 一个资产（host+port+protocol）上通常能探到多个指纹（nginx + PHP + jQuery），
//     一对多关系放在 assets 表里只能存成逗号拼接的字符串，不利于按指纹/版本聚合。
//
// 表结构由 schemas/application.sql 提供，启动时 reconcileSchema 会自动建表。
type AssetFingerprint struct {
	ID         string    `json:"id"`
	AssetID    string    `json:"asset_id"`
	Service    string    `json:"service"`
	Technology string    `json:"technology"`
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
}

// AssetFingerprintInput 是写入指纹时的输入行，不需要 ID/时间戳。
type AssetFingerprintInput struct {
	Service    string
	Technology string
	Version    string
}

const assetFingerprintSelectColumns = `id,asset_id,service,technology,version,created_at`

func normalizeAssetFingerprint(in AssetFingerprintInput) (AssetFingerprintInput, bool) {
	out := AssetFingerprintInput{
		Service:    strings.ToLower(strings.TrimSpace(in.Service)),
		Technology: strings.ToLower(strings.TrimSpace(in.Technology)),
		Version:    strings.TrimSpace(in.Version),
	}
	// 至少要有服务名或技术名，否则是一条空记录，没有存储价值。
	if out.Service == "" && out.Technology == "" {
		return out, false
	}
	return out, true
}

// UpsertAssetFingerprints 幂等地写入一个资产的服务/指纹记录。
// 依赖 UNIQUE(asset_id,service,technology,version) 与 INSERT OR IGNORE，
// 因此同一批数据被反复上报（每次扫描都会重新解析）不会产生重复行。
func (db *DB) UpsertAssetFingerprints(assetID string, items []AssetFingerprintInput) error {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" || len(items) == 0 {
		return nil
	}
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		in, ok := normalizeAssetFingerprint(raw)
		if !ok {
			continue
		}
		key := in.Service + "\x00" + in.Technology + "\x00" + in.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := tx.Exec(`INSERT OR IGNORE INTO asset_fingerprints
			(id,asset_id,service,technology,version,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?)`,
			uuid.NewString(), assetID, in.Service, in.Technology, in.Version, now, now); err != nil {
			return fmt.Errorf("写入资产指纹失败: %w", err)
		}
	}
	return tx.Commit()
}

// ListAssetFingerprints 批量读取多个资产的指纹，返回 assetID -> 指纹列表。
// 分批查询，避免 SQLite 单条语句的变量数量上限（默认 999）。
func (db *DB) ListAssetFingerprints(assetIDs []string) (map[string][]AssetFingerprint, error) {
	result := make(map[string][]AssetFingerprint)
	unique := make([]string, 0, len(assetIDs))
	seen := make(map[string]bool, len(assetIDs))
	for _, id := range assetIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	const batchSize = 400
	for start := 0; start < len(unique); start += batchSize {
		end := start + batchSize
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]interface{}, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := db.Query(`SELECT `+assetFingerprintSelectColumns+` FROM asset_fingerprints
			WHERE asset_id IN (`+placeholders+`) ORDER BY service,technology,version`, args...)
		if err != nil {
			return nil, fmt.Errorf("查询资产指纹失败: %w", err)
		}
		for rows.Next() {
			var fp AssetFingerprint
			if err := rows.Scan(&fp.ID, &fp.AssetID, &fp.Service, &fp.Technology, &fp.Version, &fp.CreatedAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("读取资产指纹失败: %w", err)
			}
			result[fp.AssetID] = append(result[fp.AssetID], fp)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}

// CountAssetFingerprints 返回某资产的指纹数量，供测试与统计使用。
func (db *DB) CountAssetFingerprints(assetID string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM asset_fingerprints WHERE asset_id=?`, strings.TrimSpace(assetID)).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}
