package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AssetHostIP 记录「域名 -> IP」的解析关系，用于资产视图的「关联IP」列。
//
// 为什么单独建表：assets 表一条记录只存一个 IP（且去重键是 host+port+protocol），
// 同一个子域名有多个 A 记录时会被合并覆盖，无法展示「一个域名关联多个 IP」。
// 这里按 (host, ip, record_type) 唯一约束累积，天然支持多值且重复扫描幂等。
type AssetHostIP struct {
	ID          string    `json:"id"`
	Host        string    `json:"host"`
	IP          string    `json:"ip"`
	RecordType  string    `json:"record_type"`
	Source      string    `json:"source"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

const assetHostIPSelectColumns = `id,host,ip,record_type,source,first_seen_at,last_seen_at`

// UpsertAssetHostIPs 幂等写入域名解析关系。
// 同一 (host, ip, record_type) 重复上报只更新 last_seen_at 与 source。
func (db *DB) UpsertAssetHostIPs(records []AssetHostIP) error {
	if len(records) == 0 {
		return nil
	}
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	seen := make(map[string]bool, len(records))
	for _, record := range records {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Host), "."))
		ip := strings.TrimSpace(record.IP)
		recordType := strings.ToUpper(strings.TrimSpace(record.RecordType))
		source := strings.TrimSpace(record.Source)
		if host == "" || ip == "" {
			continue
		}
		if recordType == "" {
			recordType = "A"
		}
		key := host + "\x00" + ip + "\x00" + recordType
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := tx.Exec(`INSERT INTO asset_host_ips
			(id,host,ip,record_type,source,first_seen_at,last_seen_at)
			VALUES (?,?,?,?,?,?,?)
			ON CONFLICT(host,ip,record_type) DO UPDATE SET
				last_seen_at=excluded.last_seen_at,
				source=CASE WHEN excluded.source<>'' THEN excluded.source ELSE asset_host_ips.source END`,
			uuid.NewString(), host, ip, recordType, source, now, now); err != nil {
			return fmt.Errorf("写入域名解析关系失败: %w", err)
		}
	}
	return tx.Commit()
}

// ListAssetHostIPs 批量读取多个域名的解析关系，返回 host -> 记录列表。
func (db *DB) ListAssetHostIPs(hosts []string) (map[string][]AssetHostIP, error) {
	result := make(map[string][]AssetHostIP)
	unique := make([]string, 0, len(hosts))
	seen := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		unique = append(unique, host)
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
		for _, host := range batch {
			args = append(args, host)
		}
		rows, err := db.Query(`SELECT `+assetHostIPSelectColumns+` FROM asset_host_ips
			WHERE host IN (`+placeholders+`) ORDER BY host,ip`, args...)
		if err != nil {
			return nil, fmt.Errorf("查询域名解析关系失败: %w", err)
		}
		for rows.Next() {
			var item AssetHostIP
			if err := rows.Scan(&item.ID, &item.Host, &item.IP, &item.RecordType, &item.Source,
				&item.FirstSeenAt, &item.LastSeenAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("读取域名解析关系失败: %w", err)
			}
			result[item.Host] = append(result[item.Host], item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}
