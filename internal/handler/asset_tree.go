package handler

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/net/publicsuffix"
)

// 资产层级视图的响应结构：公司 -> 域名 -> 子域名 -> IP -> 端口 -> 服务 -> 指纹/版本。
//
// 「公司」取资产绑定的项目名（未绑定项目归入「未绑定项目」）；
// 「域名」为有效二级域（eTLD+1），子域名为资产实际 Host；
// 纯 IP 资产的子域名层为空字符串，前端应跳过该层直接展示 IP。
type assetTreeResponse struct {
	TotalAssets int                `json:"total_assets"`
	Companies   []assetTreeCompany `json:"companies"`
}

type assetTreeCompany struct {
	CompanyID   string            `json:"company_id"`
	CompanyName string            `json:"company_name"`
	AssetCount  int               `json:"asset_count"`
	Domains     []assetTreeDomain `json:"domains"`
}

type assetTreeDomain struct {
	Domain     string               `json:"domain"`
	AssetCount int                  `json:"asset_count"`
	Subdomains []assetTreeSubdomain `json:"subdomains"`
}

type assetTreeSubdomain struct {
	Host       string        `json:"host"`
	AssetCount int           `json:"asset_count"`
	IPs        []assetTreeIP `json:"ips"`
}

type assetTreeIP struct {
	IP         string          `json:"ip"`
	AssetCount int             `json:"asset_count"`
	Ports      []assetTreePort `json:"ports"`
}

type assetTreePort struct {
	Port         int                   `json:"port"`
	Protocol     string                `json:"protocol"`
	Service      string                `json:"service"`
	AssetIDs     []string              `json:"asset_ids"`
	Technologies []assetTreeTechnology `json:"technologies"`
}

type assetTreeTechnology struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

const (
	assetTreePageSize = 2000
	assetTreeMaxRows  = 20000
)

// Tree 返回按「公司/域名/子域名/IP/端口/服务/指纹」聚合的资产层级数据。
//
// 查询参数与 GET /api/assets 保持一致（除分页由服务端内部处理），
// 因此资产列表页上的筛选条件可以直接复用到层级视图。
func (h *AssetHandler) Tree(c *gin.Context) {
	filter, err := assetListFilterFromQuery(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	access := assetAccess(c)

	// 分页拉全量：层级聚合必须要看到完整结果集，但设上限避免超大库拖垮内存。
	items := make([]*database.Asset, 0, assetTreePageSize)
	for offset := 0; offset < assetTreeMaxRows; offset += assetTreePageSize {
		batch, total, err := h.db.ListAssets(assetTreePageSize, offset, filter, access)
		if err != nil {
			h.logger.Error("资产层级视图：查询资产失败", zap.Error(err))
			c.JSON(500, gin.H{"error": "查询资产失败"})
			return
		}
		items = append(items, batch...)
		if len(batch) < assetTreePageSize || len(items) >= total {
			break
		}
	}

	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	fingerprints, err := h.db.ListAssetFingerprints(ids)
	if err != nil {
		// 指纹只是增强信息，取不到也不应该让层级视图整体失败。
		h.logger.Warn("资产层级视图：查询指纹失败，降级为无指纹展示", zap.Error(err))
		fingerprints = nil
	}

	// 公司层优先用「主域名 -> 公司」归属映射；未映射的域名回退到项目名。
	// 同时按公司名建索引：当项目名与公司同名时（常见做法），把它们并到同一个公司节点，
	// 避免同一个名字在层级里出现两次。
	index := &assetTreeCompanyIndex{ByDomain: map[string]database.Company{}, ByName: map[string]database.Company{}}
	if byDomain, err := h.db.DomainCompanyMap(); err != nil {
		h.logger.Warn("资产层级视图：查询公司归属失败，降级为按项目分组", zap.Error(err))
	} else {
		index.ByDomain = byDomain
	}
	if companies, err := h.db.ListCompanies(); err != nil {
		h.logger.Warn("资产层级视图：查询公司列表失败", zap.Error(err))
	} else {
		for _, entity := range companies {
			if name := strings.ToLower(strings.TrimSpace(entity.Name)); name != "" {
				index.ByName[name] = entity
			}
		}
	}

	c.JSON(200, buildAssetTree(items, fingerprints, index))
}

// assetTreeCompanyIndex 是层级视图判定「公司」的索引：
// 先按资产根域名命中归属，再按项目名兜底匹配同名公司。
type assetTreeCompanyIndex struct {
	ByDomain map[string]database.Company
	ByName   map[string]database.Company
}

// buildAssetTree 把扁平资产列表聚合成层级结构。
//
// index 命中时公司层使用公司实体，否则沿用项目名（未绑定项目归入「未绑定项目」）。
func buildAssetTree(assets []*database.Asset, fingerprints map[string][]database.AssetFingerprint, index *assetTreeCompanyIndex) assetTreeResponse {
	resp := assetTreeResponse{TotalAssets: len(assets), Companies: []assetTreeCompany{}}

	companies := make(map[string]*assetTreeCompany)
	domains := make(map[string]map[string]*assetTreeDomain)
	subdomains := make(map[string]map[string]*assetTreeSubdomain)
	ips := make(map[string]map[string]*assetTreeIP)
	ports := make(map[string]map[string]*assetTreePort)

	for _, asset := range assets {
		if asset == nil {
			continue
		}
		companyKey := strings.TrimSpace(asset.ProjectID)
		companyName := companyDisplayName(asset.ProjectName)
		companyID := companyKey
		mapped, ok := database.Company{}, false
		if index != nil {
			mapped, ok = index.ByDomain[assetRootDomain(asset)]
			if (!ok || mapped.ID == "") && asset.ProjectName != "" {
				// 项目名与公司同名时并到公司节点，避免层级里出现两个同名公司。
				if entity, hit := index.ByName[strings.ToLower(strings.TrimSpace(asset.ProjectName))]; hit {
					mapped, ok = entity, true
				}
			}
		}
		if ok && mapped.ID != "" {
			companyKey = "company:" + mapped.ID
			companyName = mapped.Name
			companyID = mapped.ID
		}
		company, ok := companies[companyKey]
		if !ok {
			company = &assetTreeCompany{
				CompanyID:   companyID,
				CompanyName: companyName,
				Domains:     []assetTreeDomain{},
			}
			companies[companyKey] = company
			domains[companyKey] = make(map[string]*assetTreeDomain)
		}
		company.AssetCount++

		// --- 域名层（有效二级域） ---
		domainName := assetRootDomain(asset)
		domainMap, ok := domains[companyKey]
		if !ok {
			domainMap = make(map[string]*assetTreeDomain)
			domains[companyKey] = domainMap
		}
		domain, ok := domainMap[domainName]
		if !ok {
			domain = &assetTreeDomain{Domain: domainName, Subdomains: []assetTreeSubdomain{}}
			domainMap[domainName] = domain
			subdomains[companyKey+"\x00"+domainName] = make(map[string]*assetTreeSubdomain)
		}
		domain.AssetCount++

		// --- 子域名层（资产实际 Host；纯 IP 资产为空） ---
		subKey := companyKey + "\x00" + domainName
		subName := assetSubdomain(asset)
		subMap := subdomains[subKey]
		if subMap == nil {
			subMap = make(map[string]*assetTreeSubdomain)
			subdomains[subKey] = subMap
		}
		sub, ok := subMap[subName]
		if !ok {
			sub = &assetTreeSubdomain{Host: subName, IPs: []assetTreeIP{}}
			subMap[subName] = sub
			ips[subKey+"\x00"+subName] = make(map[string]*assetTreeIP)
		}
		sub.AssetCount++

		// --- IP 层 ---
		ipKey := subKey + "\x00" + subName
		ipValue := assetIPAddress(asset)
		ipMap := ips[ipKey]
		if ipMap == nil {
			ipMap = make(map[string]*assetTreeIP)
			ips[ipKey] = ipMap
		}
		ipNode, ok := ipMap[ipValue]
		if !ok {
			ipNode = &assetTreeIP{IP: ipValue, Ports: []assetTreePort{}}
			ipMap[ipValue] = ipNode
			ports[ipKey+"\x00"+ipValue] = make(map[string]*assetTreePort)
		}
		ipNode.AssetCount++

		// --- 端口 / 服务 / 指纹层 ---
		portKey := ipKey + "\x00" + ipValue
		protocol := strings.ToLower(strings.TrimSpace(asset.Protocol))
		portIndex := fmt.Sprintf("%d\x00%s", asset.Port, protocol)
		portMap := ports[portKey]
		if portMap == nil {
			portMap = make(map[string]*assetTreePort)
			ports[portKey] = portMap
		}
		portNode, ok := portMap[portIndex]
		if !ok {
			portNode = &assetTreePort{
				Port:         asset.Port,
				Protocol:     protocol,
				AssetIDs:     []string{},
				Technologies: []assetTreeTechnology{},
			}
			portMap[portIndex] = portNode
		}
		portNode.AssetIDs = appendUniqueString(portNode.AssetIDs, asset.ID)

		// 服务名与指纹来自 asset_fingerprints；缺失时用 Server/协议兜底，
		// 保证层级视图里「服务」「指纹」两栏永远有可读内容。
		services := make([]string, 0, 1)
		techSeen := make(map[string]bool, len(portNode.Technologies))
		for _, tech := range portNode.Technologies {
			techSeen[tech.Name+"\x00"+tech.Version] = true
		}
		for _, fp := range fingerprints[asset.ID] {
			if fp.Service != "" {
				if !containsString(services, fp.Service) {
					services = append(services, fp.Service)
				}
			}
			if fp.Technology == "" {
				continue
			}
			key := fp.Technology + "\x00" + fp.Version
			if techSeen[key] {
				continue
			}
			techSeen[key] = true
			portNode.Technologies = append(portNode.Technologies, assetTreeTechnology{Name: fp.Technology, Version: fp.Version})
		}
		if len(services) == 0 && strings.TrimSpace(asset.Server) != "" {
			services = append(services, strings.ToLower(strings.TrimSpace(asset.Server)))
		}
		if len(services) == 0 && protocol != "" {
			services = append(services, protocol)
		}
		if asset.Server != "" && len(portNode.Technologies) == 0 {
			portNode.Technologies = append(portNode.Technologies, assetTreeTechnology{Name: strings.ToLower(asset.Server)})
		}
		if len(services) > 0 {
			portNode.Service = strings.Join(services, ", ")
		}
	}

	// --- 组装 + 排序 ---
	result := make([]assetTreeCompany, 0, len(companies))
	for companyKey, company := range companies {
		domainMap := domains[companyKey]
		domainList := make([]assetTreeDomain, 0, len(domainMap))
		for domainName, domain := range domainMap {
			subMap := subdomains[companyKey+"\x00"+domainName]
			subList := make([]assetTreeSubdomain, 0, len(subMap))
			for subName, sub := range subMap {
				ipMap := ips[companyKey+"\x00"+domainName+"\x00"+subName]
				ipList := make([]assetTreeIP, 0, len(ipMap))
				for ipValue, ipNode := range ipMap {
					portMap := ports[companyKey+"\x00"+domainName+"\x00"+subName+"\x00"+ipValue]
					portList := make([]assetTreePort, 0, len(portMap))
					for _, portNode := range portMap {
						sort.Slice(portNode.Technologies, func(i, j int) bool {
							if portNode.Technologies[i].Name != portNode.Technologies[j].Name {
								return portNode.Technologies[i].Name < portNode.Technologies[j].Name
							}
							return portNode.Technologies[i].Version < portNode.Technologies[j].Version
						})
						portList = append(portList, *portNode)
					}
					sort.Slice(portList, func(i, j int) bool {
						if portList[i].Port != portList[j].Port {
							return portList[i].Port < portList[j].Port
						}
						return portList[i].Protocol < portList[j].Protocol
					})
					ipCopy := *ipNode
					ipCopy.Ports = portList
					ipList = append(ipList, ipCopy)
				}
				sort.Slice(ipList, func(i, j int) bool { return ipList[i].IP < ipList[j].IP })
				subCopy := *sub
				subCopy.IPs = ipList
				subList = append(subList, subCopy)
			}
			sort.Slice(subList, func(i, j int) bool {
				if subList[i].AssetCount != subList[j].AssetCount {
					return subList[i].AssetCount > subList[j].AssetCount
				}
				return subList[i].Host < subList[j].Host
			})
			domainCopy := *domain
			domainCopy.Subdomains = subList
			domainList = append(domainList, domainCopy)
		}
		sort.Slice(domainList, func(i, j int) bool {
			if domainList[i].AssetCount != domainList[j].AssetCount {
				return domainList[i].AssetCount > domainList[j].AssetCount
			}
			return domainList[i].Domain < domainList[j].Domain
		})
		companyCopy := *company
		companyCopy.Domains = domainList
		result = append(result, companyCopy)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].CompanyName != result[j].CompanyName {
			// 未绑定项目的分组永远排在最后，避免它压住真实项目。
			if result[i].CompanyID == "" {
				return false
			}
			if result[j].CompanyID == "" {
				return true
			}
			return result[i].CompanyName < result[j].CompanyName
		}
		return result[i].CompanyID < result[j].CompanyID
	})
	resp.Companies = result
	return resp
}

func companyDisplayName(projectName string) string {
	name := strings.TrimSpace(projectName)
	if name == "" {
		return "未绑定项目"
	}
	return name
}

// assetRootDomain 返回资产的有效二级域（eTLD+1）；纯 IP 资产返回空字符串。
func assetRootDomain(asset *database.Asset) string {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(asset.Domain)), ".")
	if domain != "" {
		return domain
	}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(asset.Host)), ".")
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	if root, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return root
	}
	return host
}

// assetSubdomain 返回子域名层展示的主机名；纯 IP 资产返回空字符串。
func assetSubdomain(asset *database.Asset) string {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(asset.Host)), ".")
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	return host
}

// assetIPAddress 返回 IP 层展示的地址，缺失时回退到 IP 形态的 Host。
func assetIPAddress(asset *database.Asset) string {
	if ip := strings.TrimSpace(asset.IP); ip != "" {
		return ip
	}
	host := strings.TrimSpace(asset.Host)
	if net.ParseIP(host) != nil {
		return host
	}
	return ""
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func appendUniqueString(items []string, value string) []string {
	if value == "" || containsString(items, value) {
		return items
	}
	return append(items, value)
}
