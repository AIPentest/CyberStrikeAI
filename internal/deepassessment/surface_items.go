package deepassessment

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const SurfaceItemPageSize = 20

type SurfaceItemInput struct {
	Kind     string `json:"kind"`
	Summary  string `json:"summary"`
	Location string `json:"location"`
	FactID   string `json:"fact_id"`
}

type SurfaceItem struct {
	SurfaceItemInput
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) SurfaceItems(taskID string, offset int) ([]SurfaceItem, error) {
	rows, err := s.DB.Query(`SELECT id,kind,summary,location,COALESCE(fact_id,''),revision,updated_at
		FROM deep_assessment_surface_items WHERE task_id=? ORDER BY updated_at DESC,id LIMIT ? OFFSET ?`,
		taskID, SurfaceItemPageSize+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SurfaceItem{}
	for rows.Next() {
		var item SurfaceItem
		if err = rows.Scan(&item.ID, &item.Kind, &item.Summary, &item.Location, &item.FactID, &item.Revision, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveSurfaceItem(taskID, itemID string, revision int64, input SurfaceItemInput) (SurfaceItem, error) {
	switch input.Kind {
	case "cloud_access_key", "credential", "api_token", "other":
	default:
		return SurfaceItem{}, fmt.Errorf("请选择有效的攻击面线索类型")
	}
	input.Summary = strings.TrimSpace(input.Summary)
	input.Location = strings.TrimSpace(input.Location)
	input.FactID = strings.TrimSpace(input.FactID)
	if input.Summary == "" || utf8.RuneCountInString(input.Summary) > 120 || utf8.RuneCountInString(input.Location) > 240 {
		return SurfaceItem{}, fmt.Errorf("请填写 120 字以内的脱敏摘要，发现位置最多 240 字")
	}
	if itemID != "" && revision < 1 {
		return SurfaceItem{}, fmt.Errorf("缺少线索版本，请刷新后重试")
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return SurfaceItem{}, err
	}
	defer tx.Rollback()
	var archived bool
	if err = tx.QueryRow(`SELECT COALESCE(json_extract(payload,'$.archived'),0) FROM deep_assessments WHERE id=?`, taskID).Scan(&archived); err != nil {
		return SurfaceItem{}, err
	}
	if archived {
		return SurfaceItem{}, ErrConflict
	}
	if input.FactID != "" {
		var allowed bool
		err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM deep_assessment_facts WHERE id=? AND task_id=?)`, input.FactID, taskID).Scan(&allowed)
		if err != nil {
			return SurfaceItem{}, err
		}
		if !allowed {
			return SurfaceItem{}, fmt.Errorf("关联事实必须属于本次测评")
		}
	}

	item := SurfaceItem{SurfaceItemInput: input, ID: itemID, UpdatedAt: time.Now().UTC()}
	if itemID == "" {
		item.ID = uuid.NewString()
		item.Revision = 1
		_, err = tx.Exec(`INSERT INTO deep_assessment_surface_items(id,task_id,kind,summary,location,fact_id,revision,updated_at)
			VALUES(?,?,?,?,?,NULLIF(?,''),?,?)`, item.ID, taskID, item.Kind, item.Summary, item.Location, item.FactID, item.Revision, item.UpdatedAt)
	} else {
		var current int64
		if err = tx.QueryRow(`SELECT revision FROM deep_assessment_surface_items WHERE id=? AND task_id=?`, itemID, taskID).Scan(&current); err != nil {
			return SurfaceItem{}, err
		}
		if current != revision {
			return SurfaceItem{}, ErrConflict
		}
		item.Revision = current + 1
		_, err = tx.Exec(`UPDATE deep_assessment_surface_items SET kind=?,summary=?,location=?,fact_id=NULLIF(?,''),revision=?,updated_at=?
			WHERE id=? AND task_id=?`, item.Kind, item.Summary, item.Location, item.FactID, item.Revision, item.UpdatedAt, item.ID, taskID)
	}
	if err != nil {
		return SurfaceItem{}, err
	}
	if err = tx.Commit(); err != nil {
		return SurfaceItem{}, err
	}
	return item, nil
}
