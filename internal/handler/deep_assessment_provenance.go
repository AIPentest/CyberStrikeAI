package handler

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func assessmentEvidenceAssetAccess(c *gin.Context) database.RBACListAccess {
	if !security.SessionHasPermission(c, "asset:read") {
		return database.RBACListAccess{}
	}
	session, _ := security.CurrentSession(c)
	return database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("asset:read")}
}

func (h *AgentHandler) evidenceAsset(c *gin.Context, task *deepassessment.Task, id string) (string, bool) {
	access := assessmentEvidenceAssetAccess(c)
	if access.UserID == "" {
		c.JSON(403, gin.H{"error": "缺少资产读取权限"})
		return "", false
	}
	assets, err := h.db.AssessmentEvidenceAssets(task.ID, task.ProjectID, []string{id}, access)
	if err != nil {
		deepAssessmentError(c, err)
		return "", false
	}
	title, ok := assets[id]
	if !ok {
		deepAssessmentError(c, sql.ErrNoRows)
	}
	return title, ok
}

func evidenceOffset(c *gin.Context) int {
	value, _ := strconv.Atoi(c.Query("offset"))
	return min(max(0, value), int(^uint(0)>>1)-100)
}

func (h *AgentHandler) DeepAssessmentEvidencePath(c *gin.Context) {
	store := h.deepAssessments.Store
	task, err := store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	criterion, err := strconv.Atoi(c.DefaultQuery("criterion", "0"))
	focus := deepassessment.EvidenceFocus{
		AssetID: c.Query("asset_id"), EndpointID: c.Query("endpoint_id"), FactID: c.Query("fact_id"),
		Criterion: criterion, Offset: evidenceOffset(c),
	}
	var selectors int
	for _, set := range []bool{focus.AssetID != "", focus.FactID != "", criterion > 0} {
		if set {
			selectors++
		}
	}
	if err != nil || selectors != 1 || criterion < 0 || (focus.EndpointID != "" && focus.AssetID == "") {
		c.JSON(400, gin.H{"error": "请选择一个资产、事实或验收项"})
		return
	}
	access := assessmentEvidenceAssetAccess(c)
	if focus.AssetID != "" && access.UserID == "" {
		c.JSON(403, gin.H{"error": "缺少资产读取权限"})
		return
	}
	view, err := store.EvidencePath(task, access, focus)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.JSON(200, view)
}

func (h *AgentHandler) DeepAssessmentEndpointEvidence(c *gin.Context) {
	store := h.deepAssessments.Store
	task, err := store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	if c.Request.Method == "PUT" {
		var input struct {
			AssetID    string `json:"asset_id"`
			EndpointID string `json:"endpoint_id"`
			FactID     string `json:"fact_id"`
			Linked     *bool  `json:"linked"`
		}
		if !deepAssessmentBody(c, &input) {
			return
		}
		if input.AssetID == "" || input.EndpointID == "" || input.FactID == "" || input.Linked == nil {
			c.JSON(400, gin.H{"error": "缺少资产、端点、事实或关联状态"})
			return
		}
		if _, ok := h.evidenceAsset(c, task, input.AssetID); !ok {
			return
		}
		if err = store.SetEndpointEvidence(task.ID, input.AssetID, input.EndpointID, input.FactID, *input.Linked); err != nil {
			deepAssessmentError(c, err)
			return
		}
		if h.audit != nil {
			h.audit.RecordOK(c, "task", "deep_assessment_endpoint_evidence", "更新端点证据", "deep_assessment", task.ID,
				gin.H{"asset_id": input.AssetID, "endpoint_id": input.EndpointID, "fact_id": input.FactID, "linked": *input.Linked})
		}
		c.JSON(200, gin.H{"saved": true})
		return
	}
	asset := c.Query("asset_id")
	title, ok := h.evidenceAsset(c, task, asset)
	if !ok {
		return
	}
	offset := evidenceOffset(c)
	if id := c.Query("endpoint_id"); id != "" {
		endpoint, err := store.EvidenceEndpoint(asset, id)
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		// Page all facts for the parent asset, overlaying their explicit links.
		facts, err := store.EvidenceAssetFacts(task.ID, asset, "", offset, 21)
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		var items []gin.H
		for _, fact := range facts[:min(len(facts), 20)] {
			var linked bool
			if err = h.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM deep_assessment_endpoint_evidence WHERE endpoint_id=? AND fact_id=?)`, id, fact.ID).Scan(&linked); err != nil {
				deepAssessmentError(c, err)
				return
			}
			items = append(items, gin.H{"fact": fact, "linked": linked})
		}
		c.JSON(200, gin.H{"endpoint": endpoint, "title": title, "items": items, "has_more": len(facts) > 20})
		return
	}
	endpoints, counts, more, err := store.EndpointEvidence(task.ID, asset, c.Query("status"), offset)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"title": title, "endpoints": endpoints, "counts": counts, "has_more": more})
}

func (h *AgentHandler) DeepAssessmentEvidenceHistory(c *gin.Context) {
	store := h.deepAssessments.Store
	task, err := store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	asset := c.Query("asset_id")
	title, ok := h.evidenceAsset(c, task, asset)
	if !ok {
		return
	}
	session, _ := security.CurrentSession(c)
	history, err := store.AssetEvidenceHistory(task, asset, title, database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("assessments:read")}, evidenceOffset(c), 10)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.JSON(200, history)
}

func (h *AgentHandler) appendEvidenceComparisonReport(c *gin.Context, task *deepassessment.Task, b *strings.Builder) error {
	access := assessmentEvidenceAssetAccess(c)
	if access.UserID == "" {
		return nil
	}
	session, _ := security.CurrentSession(c)
	historyAccess := database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("assessments:read")}
	store := h.deepAssessments.Store
	const page = 50
	b.WriteString("\n## 端点证据与历次测评对照\n\n按明确关联的事实统计端点。历史差异基于最近一条可读的较早测评记录及当前已保存的证据。新增观察按事实摘要与正文的原文比对。\n")
	for offset := 0; ; offset += page {
		assets, total, err := h.db.ListAssets(page, offset, database.AssetListFilter{AssessmentID: task.ID, ProjectID: task.ProjectID}, access)
		if err != nil {
			return err
		}
		for _, asset := range assets {
			_, counts, _, err := store.EndpointEvidence(task.ID, asset.ID, "", 0)
			if err != nil {
				return err
			}
			history, err := store.AssetEvidenceHistory(task, asset.ID, asset.Host, historyAccess, 0, 1)
			if err != nil {
				return err
			}
			title := asset.Host
			if title == "" {
				title = asset.ID
			}
			reportLiteral(b, title)
			suffix := "&asset_id=" + url.QueryEscape(asset.ID)
			fmt.Fprintf(b, "- 已有观察证据 %d 个端点，待人工复核 %d 个，缺证据 %d 个。[端点缺口](%s%s&endpoint_state=missing)\n",
				counts["observed"], counts["needs_review"], counts["missing"], assessmentReportLink(c, task.ID, "", "endpoint-review"), suffix)
			fmt.Fprintf(b, "- 本次已保存有效观察 %d 条。[历次测评](%s%s)\n", history.Current.ObservedFacts, assessmentReportLink(c, task.ID, "", "asset-history"), suffix)
			for _, number := range history.Current.PendingCriteria {
				fmt.Fprintf(b, "- [待人工复核的验收项 %d](%s&criterion=%d)\n", number, assessmentReportLink(c, task.ID, "", "acceptance"), number)
			}
			if len(history.Prior) > 0 {
				prior := history.Prior[0]
				if prior.Comparable {
					fmt.Fprintf(b, "- 对照前次记录，本次新增观察文本 %d 条，新增证据端点 %d 个，前次有证据而本次暂缺记录 %d 个。\n", prior.NewObservations, prior.NewEndpoints, prior.UnseenNow)
				} else {
					b.WriteString("- 前次测评范围或项目口径不同，无法直接比较。\n")
				}
			}
		}
		if offset+len(assets) >= total || len(assets) == 0 {
			return nil
		}
	}
}
