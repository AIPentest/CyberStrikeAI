package handler

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/deepassessment"
	"github.com/gin-gonic/gin"
)

func (h *AgentHandler) DeepAssessmentAcceptance(c *gin.Context) {
	store := h.deepAssessments.Store
	task, err := store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	matrix, err := store.AcceptanceMatrix(task)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"rows": matrix})
}

func (h *AgentHandler) DeepAssessmentAcceptanceSources(c *gin.Context) {
	offset, _ := strconv.Atoi(c.Query("offset"))
	offset = min(max(0, offset), int(^uint(0)>>1)-deepassessment.AcceptanceSourcePageSize)
	sources, err := h.deepAssessments.Store.AcceptanceSources(c.Param("id"), c.Query("kind"), c.Query("q"), offset)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	hasMore := len(sources) > deepassessment.AcceptanceSourcePageSize
	sources = sources[:min(len(sources), deepassessment.AcceptanceSourcePageSize)]
	c.JSON(200, gin.H{"sources": sources, "has_more": hasMore, "next_offset": offset + len(sources)})
}

func (h *AgentHandler) SaveDeepAssessmentAcceptance(c *gin.Context) {
	number, _ := strconv.Atoi(c.Param("criterion"))
	var mapping deepassessment.AcceptanceMapping
	if !deepAssessmentBody(c, &mapping) {
		return
	}
	store := h.deepAssessments.Store
	task, err := store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	if err = store.SaveAcceptanceMapping(task, number, mapping); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "deep_assessment_acceptance", "更新验收证据关联", "deep_assessment", task.ID, gin.H{"criterion": number, "needs_review": mapping.NeedsReview})
	}
	c.JSON(200, gin.H{"saved": true})
}

func (h *AgentHandler) appendAcceptanceReport(c *gin.Context, task *deepassessment.Task, b *strings.Builder) error {
	matrix, err := h.deepAssessments.Store.AcceptanceMatrix(task)
	if err != nil {
		return err
	}
	labels := map[string]string{"has_evidence": "已有证据", "missing": "缺证据", "needs_review": "待人工复核"}
	counts := map[string]int{}
	for _, row := range matrix {
		counts[row.Status]++
	}
	fmt.Fprintf(b, "\n## 验收证据矩阵\n\n已有证据 %d 项，缺证据 %d 项，待人工复核 %d 项。状态反映当前证据关联情况，验收结论由审阅人核对。\n",
		counts["has_evidence"], counts["missing"], counts["needs_review"])
	matrixURL := assessmentReportLink(c, task.ID, "", "acceptance")
	pathURL := assessmentReportLink(c, task.ID, "", "evidence-path")
	graphURL := assessmentReportLink(c, task.ID, "", "graph")
	receiptsURL := assessmentReportLink(c, task.ID, "", "evidence")
	inline := strings.NewReplacer("\\", "\\\\", "\r", " ", "\n", " ", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "`", "\\`")
	for _, row := range matrix {
		fmt.Fprintf(b, "\n### 验收项 %d · %s\n", row.Number, labels[row.Status])
		reportLiteral(b, row.Text)
		fmt.Fprintf(b, "\n[在工作台查看验收项](%s&criterion=%d)\n", matrixURL, row.Number)
		fmt.Fprintf(b, "\n[查看局部证据图](%s&criterion=%d)\n", pathURL, row.Number)
		receipts := make(map[string]deepassessment.AcceptanceSource, len(row.Receipts))
		for _, receipt := range row.Receipts {
			receipts[receipt.ID] = receipt
		}
		writeReceipt := func(id, indent, label string) {
			if receipt, ok := receipts[id]; ok {
				fmt.Fprintf(b, "%s- [%s %s](%s&receipt_id=%s) · %s\n", indent, label, inline.Replace(id), receiptsURL, url.QueryEscape(id), receipt.State)
			}
		}
		for _, fact := range row.Facts {
			fmt.Fprintf(b, "- [事实 %s](%s&graph_view=full&node_id=%s) · %s · %s\n", inline.Replace(fact.ID), graphURL, url.QueryEscape(fact.ID), fact.State, inline.Replace(fact.Title))
			for _, id := range fact.ReceiptIDs {
				writeReceipt(id, "  ", "原始回执")
			}
		}
		for _, id := range row.ReceiptIDs {
			writeReceipt(id, "", "直接关联回执")
		}
		if row.ReviewNote != "" {
			b.WriteString("\n复核说明\n")
			reportLiteral(b, row.ReviewNote)
		}
	}
	return nil
}
