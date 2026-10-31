package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// 站点截图的元信息。图片本体落在文件系统（ScreenshotConfig.Dir），
// 这里只保存状态与路径，便于排队、轮询与鉴权下发。
const (
	AssetScreenshotStatusPending = "pending"
	AssetScreenshotStatusRunning = "running"
	AssetScreenshotStatusDone    = "done"
	AssetScreenshotStatusFailed  = "failed"
)

type AssetScreenshot struct {
	AssetID    string     `json:"asset_id"`
	Status     string     `json:"status"`
	FilePath   string     `json:"file_path"`
	StatusCode int        `json:"status_code"`
	Title      string     `json:"title"`
	Error      string     `json:"error"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	CapturedAt *time.Time `json:"captured_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

const assetScreenshotColumns = `asset_id,status,file_path,status_code,title,error,width,height,captured_at,updated_at`

func scanAssetScreenshot(scanner interface{ Scan(...interface{}) error }) (*AssetScreenshot, error) {
	var item AssetScreenshot
	var capturedAt interface{}
	if err := scanner.Scan(&item.AssetID, &item.Status, &item.FilePath, &item.StatusCode,
		&item.Title, &item.Error, &item.Width, &item.Height, &capturedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if parsed, ok := parseAssetScanTime(capturedAt); ok {
		item.CapturedAt = &parsed
	}
	return &item, nil
}

// GetAssetScreenshot 读取单个资产的截图元信息；不存在时返回 nil, nil。
func (db *DB) GetAssetScreenshot(assetID string) (*AssetScreenshot, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return nil, nil
	}
	row := db.QueryRow(`SELECT `+assetScreenshotColumns+` FROM asset_screenshots WHERE asset_id=?`, assetID)
	item, err := scanAssetScreenshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取截图元信息失败: %w", err)
	}
	return item, nil
}

// ListAssetScreenshots 批量读取截图元信息，返回 assetID -> 元信息。
func (db *DB) ListAssetScreenshots(assetIDs []string) (map[string]*AssetScreenshot, error) {
	result := make(map[string]*AssetScreenshot, len(assetIDs))
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
		rows, err := db.Query(`SELECT `+assetScreenshotColumns+` FROM asset_screenshots WHERE asset_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("查询截图元信息失败: %w", err)
		}
		for rows.Next() {
			item, err := scanAssetScreenshot(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("读取截图元信息失败: %w", err)
			}
			result[item.AssetID] = item
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}

// SaveAssetScreenshot 写入（或覆盖）一条截图元信息。
func (db *DB) SaveAssetScreenshot(item *AssetScreenshot) error {
	if item == nil || strings.TrimSpace(item.AssetID) == "" {
		return nil
	}
	if item.Status == "" {
		item.Status = AssetScreenshotStatusPending
	}
	now := time.Now()
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	_, err := db.Exec(`INSERT INTO asset_screenshots
		(asset_id,status,file_path,status_code,title,error,width,height,captured_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(asset_id) DO UPDATE SET
			status=excluded.status,
			file_path=CASE WHEN excluded.file_path<>'' THEN excluded.file_path ELSE asset_screenshots.file_path END,
			status_code=excluded.status_code,
			title=CASE WHEN excluded.title<>'' THEN excluded.title ELSE asset_screenshots.title END,
			error=excluded.error,
			width=CASE WHEN excluded.width>0 THEN excluded.width ELSE asset_screenshots.width END,
			height=CASE WHEN excluded.height>0 THEN excluded.height ELSE asset_screenshots.height END,
			captured_at=COALESCE(excluded.captured_at, asset_screenshots.captured_at),
			updated_at=excluded.updated_at`,
		item.AssetID, item.Status, item.FilePath, item.StatusCode, item.Title, item.Error,
		item.Width, item.Height, item.CapturedAt, item.UpdatedAt)
	if err != nil {
		return fmt.Errorf("保存截图元信息失败: %w", err)
	}
	return nil
}

// MarkAssetScreenshotRunning 把资产截图状态置为 running（排队出队时调用）。
func (db *DB) MarkAssetScreenshotRunning(assetID string) error {
	return db.SaveAssetScreenshot(&AssetScreenshot{
		AssetID:   strings.TrimSpace(assetID),
		Status:    AssetScreenshotStatusRunning,
		UpdatedAt: time.Now(),
	})
}
