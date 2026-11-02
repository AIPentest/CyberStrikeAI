package handler

import (
	"database/sql"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/company"
	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 公司 / 主域名面板接口。
//
// 与「项目」解耦：公司是长期存在的组织实体，资产层级视图的「公司」层
// 通过 domain -> company 归属映射得出，未映射的域名回退到项目名。

type CompanyHandler struct {
	db       *database.DB
	lookuper *company.Lookuper
	logger   *zap.Logger
}

func NewCompanyHandler(db *database.DB, lookuper *company.Lookuper, logger *zap.Logger) *CompanyHandler {
	return &CompanyHandler{db: db, lookuper: lookuper, logger: logger}
}

type createCompanyRequest struct {
	Name    string   `json:"name" binding:"required"`
	Note    string   `json:"note"`
	Domains []string `json:"domains"` // 可选：创建时一并归属的主域名（视为手工确认）
}

type addCompanyDomainRequest struct {
	Domain string `json:"domain" binding:"required"`
	Note   string `json:"note"`
}

type confirmDomainRequest struct {
	CompanyID string `json:"company_id" binding:"required"`
}

// List 返回全部公司及其域名。
func (h *CompanyHandler) List(c *gin.Context) {
	companies, err := h.db.ListCompanies()
	if err != nil {
		h.logger.Error("查询公司失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询公司失败"})
		return
	}
	if companies == nil {
		companies = []database.Company{}
	}
	c.JSON(http.StatusOK, gin.H{"companies": companies})
}

// Create 新建公司，可同时归属若干手工填写的主域名。
func (h *CompanyHandler) Create(c *gin.Context) {
	var req createCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "公司名称不能为空"})
		return
	}
	access := assetAccess(c)
	created, err := h.db.CreateCompany(req.Name, req.Note, access.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for _, domain := range req.Domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if _, err := h.db.AddCompanyDomain(created.ID, domain, "manual", ""); err != nil {
			h.logger.Warn("创建公司时归属域名失败",
				zap.String("company_id", created.ID), zap.String("domain", domain), zap.Error(err))
		}
	}
	companies, err := h.db.ListCompanies()
	if err == nil {
		for _, item := range companies {
			if item.ID == created.ID {
				c.JSON(http.StatusCreated, gin.H{"company": item})
				return
			}
		}
	}
	c.JSON(http.StatusCreated, gin.H{"company": created})
}

// Update 修改公司名称/备注。
func (h *CompanyHandler) Update(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "公司名称不能为空"})
		return
	}
	if err := h.db.UpdateCompany(c.Param("id"), req.Name, req.Note); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "公司不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete 删除公司；其域名会变为「待归属」而不是被删除。
func (h *CompanyHandler) Delete(c *gin.Context) {
	if err := h.db.DeleteCompany(c.Param("id")); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "公司不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AddDomain 手工把一个主域名归属到公司。
func (h *CompanyHandler) AddDomain(c *gin.Context) {
	companyID := c.Param("id")
	if item, err := h.db.GetCompany(companyID); err != nil || item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "公司不存在"})
		return
	}
	var req addCompanyDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "域名不能为空"})
		return
	}
	domain, err := h.db.AddCompanyDomain(companyID, req.Domain, "manual", req.Note)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"domain": domain})
}

// RemoveDomain 解除域名与公司的归属（域名记录被删除）。
func (h *CompanyHandler) RemoveDomain(c *gin.Context) {
	if err := h.db.RemoveCompanyDomain(c.Param("id"), c.Param("domainId")); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "域名归属不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Lookup 按公司名推荐主域名（本地资产优先，其次测绘 API）。
func (h *CompanyHandler) Lookup(c *gin.Context) {
	companyID := c.Param("id")
	entity, err := h.db.GetCompany(companyID)
	if err != nil || entity == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "公司不存在"})
		return
	}
	suggestions, warnings, err := h.lookuper.Suggest(c.Request.Context(), entity.Name, 20)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"company":     entity,
		"suggestions": suggestions,
		"warnings":    warnings,
	})
}

// ListDomains 列出域名归属（?status=pending 只看待确认）。
func (h *CompanyHandler) ListDomains(c *gin.Context) {
	items, err := h.db.ListCompanyDomains(c.Query("status"))
	if err != nil {
		h.logger.Error("查询域名归属失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询域名归属失败"})
		return
	}
	if items == nil {
		items = []database.CompanyDomain{}
	}
	c.JSON(http.StatusOK, gin.H{"domains": items})
}

// ConfirmDomain 确认待归属域名的公司归属。
func (h *CompanyHandler) ConfirmDomain(c *gin.Context) {
	var req confirmDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company_id 必填"})
		return
	}
	if company, err := h.db.GetCompany(req.CompanyID); err != nil || company == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "公司不存在"})
		return
	}
	if err := h.db.SetCompanyDomainStatus(c.Param("id"), req.CompanyID, database.CompanyDomainStatusConfirmed); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "域名记录不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// IgnoreDomain 忽略一个待归属域名（以后自动采集也不会再提示）。
func (h *CompanyHandler) IgnoreDomain(c *gin.Context) {
	if err := h.db.SetCompanyDomainStatus(c.Param("id"), "", database.CompanyDomainStatusIgnored); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "域名记录不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
