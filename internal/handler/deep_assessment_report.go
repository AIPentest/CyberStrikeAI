package handler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func assessmentReportLink(c *gin.Context, task, step, tab string) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	q := url.Values{"assessment_id": {task}, "tab": {tab}}
	if step != "" {
		q.Set("step_id", step)
	}
	return scheme + "://" + c.Request.Host + "/#deep-assessments?" + q.Encode()
}
func reportLiteral(b *strings.Builder, text string) {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	fmt.Fprintf(b, "\n%s\n%s\n%s\n", fence, text, fence)
}
func (h *AgentHandler) reportReviews(task, finding string) ([]database.AssessmentFindingReview, error) {
	rows, err := h.db.Query("SELECT id,step_id,verdict,reason,evidence_json,created_at FROM deep_assessment_finding_reviews WHERE task_id=? AND vulnerability_id=? ORDER BY created_at,id", task, finding)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []database.AssessmentFindingReview{}
	for rows.Next() {
		var r database.AssessmentFindingReview
		var evidence string
		if err = rows.Scan(&r.ID, &r.StepID, &r.Verdict, &r.Reason, &evidence, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(evidence), &r.Evidence); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (h *AgentHandler) reportReceipt(b *strings.Builder, c *gin.Context, task, step, ref string) error {
	var id, tool, state, result, execution string
	err := h.db.QueryRow("SELECT id,tool,status,result,execution_id FROM deep_assessment_calls WHERE task_id=? AND step_id=? AND (id=? OR execution_id=?)", task, step, ref, ref).Scan(&id, &tool, &state, &result, &execution)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "\n**回执 %s** · %s · %s\n\n[回到来源步骤与回执](%s)\n\n执行记录：%s\n\n已保存的回执摘录（完整执行过程请打开来源会话）：\n", id, tool, state, assessmentReportLink(c, task, step, "evidence"), execution)
	reportLiteral(b, result)
	return nil
}

func (h *AgentHandler) appendAssessmentFindingsReport(c *gin.Context, t *deepassessment.Task, b *strings.Builder) error {
	b.WriteString("\n## 关联漏洞与复核\n\n以下仅包含当前账号有权查看的关联漏洞；已登记不等于已确认，复核结论单独列出。\n")
	if !security.SessionHasPermission(c, "vulnerability:read") {
		b.WriteString("\n当前账号没有漏洞读取权限，未导出漏洞详情。\n")
		return nil
	}
	session, _ := security.CurrentSession(c)
	access := database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("vulnerability:read")}
	filter := database.VulnerabilityListFilter{AssessmentID: t.ID, SummaryOnly: true}
	count := 0
	for offset := 0; ; offset += 50 {
		rows, err := h.db.ListVulnerabilitiesByGroupForAccess(50, offset, filter, access, "none", "latest")
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !h.db.UserCanAccessResource(session.UserID, access.Scope, "vulnerability", row.ID) {
				continue
			}
			v, err := h.db.GetVulnerability(row.ID)
			if err != nil {
				return err
			}
			if v.AssessmentID != t.ID {
				continue
			}
			v.AssessmentReviews, err = h.reportReviews(t.ID, v.ID)
			if err != nil {
				return err
			}
			count++
			fmt.Fprintf(b, "\n### %d. %s\n\n漏洞 ID：%s · 严重程度：%s · 状态：%s\n\n来源意图：%s · %s\n\n[测评中的漏洞与证据](%s)\n", count, strings.ReplaceAll(v.Title, "\n", " "), v.ID, v.Severity, v.Status, v.AssessmentStepID, v.AssessmentStepTitle, assessmentReportLink(c, t.ID, v.AssessmentStepID, "evidence")+"&vulnerability_id="+url.QueryEscape(v.ID))
			for _, field := range []struct{ name, text string }{{"目标", v.Target}, {"说明", v.Description}, {"前置条件", v.Preconditions}, {"复现步骤", v.ReproSteps}, {"复现证据", v.Evidence}, {"影响", v.Impact}, {"修复建议", v.Recommendation}, {"复测说明", v.RetestNotes}, {"复测历史", v.RetestLog}} {
				if field.text != "" {
					fmt.Fprintf(b, "\n#### %s\n", field.name)
					reportLiteral(b, field.text)
				}
			}
			independent := false
			for _, review := range v.AssessmentReviews {
				independent = independent || review.Verdict != "reported"
			}
			if !independent {
				b.WriteString("\n尚无独立复核记录。\n")
			}
			for _, review := range v.AssessmentReviews {
				label := "独立复核"
				if review.Verdict == "reported" {
					label = "首次登记"
				}
				fmt.Fprintf(b, "\n#### %s · %s\n\n意图：%s · 时间：%s\n\n[查看复核步骤与原始回执](%s)\n", label, review.Verdict, review.StepID, review.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), assessmentReportLink(c, t.ID, review.StepID, "evidence"))
				reportLiteral(b, review.Reason)
				for _, ref := range review.Evidence {
					if err := h.reportReceipt(b, c, t.ID, review.StepID, ref); err != nil {
						return fmt.Errorf("读取关联复核回执失败: %w", err)
					}
				}
			}
		}
		if len(rows) < 50 {
			break
		}
	}
	if count == 0 {
		b.WriteString("\n当前账号可见的关联漏洞为 0；这不代表全部资产不存在漏洞。\n")
	}
	return nil
}
