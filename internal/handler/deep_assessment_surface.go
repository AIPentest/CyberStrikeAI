package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func (h *AgentHandler) DeepAssessmentSurfaceItems(c *gin.Context) {
	if !security.SessionHasPermission(c, "asset:read") {
		c.JSON(http.StatusForbidden, gin.H{"error": "缺少攻击面图访问权限"})
		return
	}
	store := h.deepAssessments.Store
	taskID := c.Param("id")
	if c.Request.Method == http.MethodGet {
		if _, err := store.Get(taskID); err != nil {
			deepAssessmentError(c, err)
			return
		}
		offset := evidenceOffset(c)
		items, err := store.SurfaceItems(taskID, offset)
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		limit := deepassessment.SurfaceItemPageSize
		c.JSON(http.StatusOK, gin.H{"items": items[:min(len(items), limit)], "has_more": len(items) > limit})
		return
	}
	if !deepAssessmentBody(c, nil) {
		return
	}
	var input struct {
		deepassessment.SurfaceItemInput
		Revision int64 `json:"revision"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "线索只接受类型、脱敏摘要、发现位置和关联事实"})
		return
	}
	item, err := store.SaveSurfaceItem(taskID, c.Param("itemId"), input.Revision, input.SurfaceItemInput)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "deep_assessment_surface_item", "保存攻击面线索", "deep_assessment", taskID,
			gin.H{"item_id": item.ID, "kind": item.Kind, "revision": item.Revision})
	}
	c.JSON(http.StatusOK, item)
}
