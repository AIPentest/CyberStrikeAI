// Package company 提供「按公司名反查主域名」的能力：
// 先在本地已有资产里找候选（命中项目名/业务系统/部门），
// 再按需调用测绘 API（当前实现 FOFA 的 org 查询）补充。
package company

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
	"golang.org/x/net/publicsuffix"
)

// Suggestion 是「某个公司可能拥有的主域名」建议。
type Suggestion struct {
	Domain string `json:"domain"`
	Source string `json:"source"`         // local | fofa | input
	Hits   int    `json:"hits"`           // 证据数量（本地资产条数 / 测绘结果条数）
	Note   string `json:"note,omitempty"` // 补充说明，例如已归属哪个公司
}

// Lookuper 负责组合本地数据与测绘 API 生成建议。
type Lookuper struct {
	db     *database.DB
	fofa   config.FofaConfig
	client *http.Client
	logger *zap.Logger
}

func NewLookuper(db *database.DB, cfg *config.Config, logger *zap.Logger) *Lookuper {
	l := &Lookuper{
		db:     db,
		logger: logger,
		client: &http.Client{Timeout: 20 * time.Second},
	}
	if cfg != nil {
		l.fofa = cfg.FOFA
	}
	return l
}

const maxSuggestions = 20

// Suggest 返回候选主域名。warnings 用于告知前端哪些数据源不可用，
// 不属于错误（例如没配 FOFA Key 时仍可返回本地结果）。
func (l *Lookuper) Suggest(ctx context.Context, companyName string, limit int) ([]Suggestion, []string, error) {
	name := strings.TrimSpace(companyName)
	if name == "" {
		return nil, nil, fmt.Errorf("公司名称不能为空")
	}
	if limit <= 0 || limit > maxSuggestions {
		limit = maxSuggestions
	}
	var warnings []string
	merged := make(map[string]*Suggestion)

	// 0) 用户直接输入的就是域名时，优先采纳。
	if looksLikeDomain(name) {
		merged[normalizeDomain(name)] = &Suggestion{
			Domain: normalizeDomain(name),
			Source: "input",
			Hits:   1,
			Note:   "输入内容本身是一个域名",
		}
	}

	// 1) 本地已有资产
	if l.db != nil {
		local, err := l.db.LocalDomainCandidates(name, limit)
		if err != nil {
			warnings = append(warnings, "本地资产查询失败: "+err.Error())
		} else {
			for domain, hits := range local {
				if domain == "" {
					continue
				}
				merged[domain] = &Suggestion{Domain: domain, Source: "local", Hits: hits}
			}
		}
	}

	// 2) 测绘 API（FOFA org 查询）
	if strings.TrimSpace(l.fofa.APIKey) == "" {
		warnings = append(warnings, "未配置 FOFA API Key，仅返回本地结果；可在 config.yaml 的 fofa.api_key 配置后重试")
	} else {
		fofaResult, err := l.queryFOFA(ctx, name)
		if err != nil {
			warnings = append(warnings, "FOFA 查询失败: "+err.Error())
		} else {
			for domain, hits := range fofaResult {
				if existing, ok := merged[domain]; ok {
					existing.Hits += hits
					existing.Source = existing.Source + "+fofa"
					continue
				}
				merged[domain] = &Suggestion{Domain: domain, Source: "fofa", Hits: hits}
			}
		}
	}

	// 3) 标注已归属情况，避免用户重复归属
	result := make([]Suggestion, 0, len(merged))
	for domain, item := range merged {
		if l.db != nil {
			if existing, err := l.db.GetCompanyDomainByDomain(domain); err == nil && existing != nil {
				switch existing.Status {
				case database.CompanyDomainStatusConfirmed:
					// 已经归属时把公司名带上，让用户知道是「当前公司」还是别家。
					note := "已归属公司"
					if existing.CompanyID != "" {
						if entity, err := l.db.GetCompany(existing.CompanyID); err == nil && entity != nil {
							note = "已归属公司：" + entity.Name
						}
					}
					item.Note = note
				case database.CompanyDomainStatusIgnored:
					item.Note = "此前已被忽略"
				default:
					item.Note = "已在待确认列表中"
				}
			}
		}
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Hits != result[j].Hits {
			return result[i].Hits > result[j].Hits
		}
		return result[i].Domain < result[j].Domain
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, warnings, nil
}

// queryFOFA 用 org 字段按组织名搜索，统计返回结果里的根域名频次。
func (l *Lookuper) queryFOFA(ctx context.Context, companyName string) (map[string]int, error) {
	baseURL := strings.TrimSpace(l.fofa.BaseURL)
	if baseURL == "" {
		baseURL = "https://fofa.info/api/v1/search/all"
	}
	// FOFA 语法：org="公司名"，字段取 host,domain 用于提取根域名。
	query := fmt.Sprintf(`org="%s"`, strings.ReplaceAll(companyName, `"`, ""))
	params := url.Values{}
	params.Set("key", strings.TrimSpace(l.fofa.APIKey))
	params.Set("qbase64", base64.StdEncoding.EncodeToString([]byte(query)))
	params.Set("fields", "host,domain")
	params.Set("size", "100")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FOFA 返回 HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Error   bool            `json:"error"`
		ErrMsg  string          `json:"errmsg"`
		Results [][]interface{} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("解析 FOFA 响应失败: %w", err)
	}
	if parsed.Error {
		msg := strings.TrimSpace(parsed.ErrMsg)
		if msg == "" {
			msg = "FOFA 返回错误"
		}
		return nil, fmt.Errorf("%s", msg)
	}

	counts := make(map[string]int)
	for _, row := range parsed.Results {
		for _, cell := range row {
			text, ok := cell.(string)
			if !ok {
				continue
			}
			if domain := rootDomainFromCell(text); domain != "" {
				// 一行可能同时返回 host 与 domain，二者指向同一根域名：
				// 每行只计一次，否则命中次数会翻倍。
				counts[domain]++
				break
			}
		}
	}
	return counts, nil
}

// rootDomainFromCell 从 host/domain/URL 单元格里提取 eTLD+1。
func rootDomainFromCell(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
			value = parsed.Hostname()
		}
	}
	if idx := strings.IndexAny(value, ":/ "); idx >= 0 {
		value = value[:idx]
	}
	value = strings.Trim(strings.ToLower(value), "[].")
	if value == "" || !strings.Contains(value, ".") {
		return ""
	}
	// 纯 IP 不是「主域名」，不能作为公司域名建议。
	if net.ParseIP(value) != nil {
		return ""
	}
	if root, err := publicsuffix.EffectiveTLDPlusOne(value); err == nil {
		return root
	}
	return value
}

func looksLikeDomain(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t") {
		return false
	}
	return strings.Contains(value, ".") && rootDomainFromCell(value) != ""
}

func normalizeDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
