package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 公司（组织）与主域名归属。
//
// 与「项目」的关系：项目是一次测试任务的容器，公司是长期存在的组织实体，
// 一个公司可以跨多个项目。资产层级视图的「公司」层由 domain -> company 映射得出，
// 未映射的域名再回退到项目名。

const (
	CompanyDomainStatusConfirmed = "confirmed" // 已确认归属
	CompanyDomainStatusPending   = "pending"   // 自动发现，待用户确认归属
	CompanyDomainStatusIgnored   = "ignored"   // 用户已忽略
)

type Company struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Note        string          `json:"note"`
	OwnerUserID string          `json:"-"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Domains     []CompanyDomain `json:"domains,omitempty"`
	// AssetCount 该公司名下所有已确认域名的资产总数（列表查询时填充）。
	AssetCount int `json:"asset_count"`
}

type CompanyDomain struct {
	ID          string    `json:"id"`
	CompanyID   string    `json:"company_id"`
	CompanyName string    `json:"company_name,omitempty"`
	Domain      string    `json:"domain"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
	Note        string    `json:"note"`
	AssetCount  int       `json:"asset_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func normalizeCompanyDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

// CreateCompany 新建公司。name 必填且大小写不敏感唯一。
func (db *DB) CreateCompany(name, note, ownerUserID string) (*Company, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("公司名称不能为空")
	}
	now := time.Now()
	company := &Company{
		ID:          uuid.NewString(),
		Name:        name,
		Note:        strings.TrimSpace(note),
		OwnerUserID: ownerUserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := db.Exec(`INSERT INTO companies (id,name,note,owner_user_id,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
		company.ID, company.Name, company.Note, nullIfEmpty(ownerUserID), now, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("公司「%s」已存在", name)
		}
		return nil, fmt.Errorf("创建公司失败: %w", err)
	}
	return company, nil
}

// UpdateCompany 更新公司名称与备注。
func (db *DB) UpdateCompany(id, name, note string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("公司名称不能为空")
	}
	res, err := db.Exec(`UPDATE companies SET name=?,note=?,updated_at=? WHERE id=?`,
		name, strings.TrimSpace(note), time.Now(), strings.TrimSpace(id))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("公司「%s」已存在", name)
		}
		return fmt.Errorf("更新公司失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteCompany 删除公司。其域名会保留但变为「待归属」（company_id 置空、status=pending），
// 避免用户误删公司时连带丢失已发现的主域名。
func (db *DB) DeleteCompany(id string) error {
	id = strings.TrimSpace(id)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE company_domains SET company_id=NULL,status=?,updated_at=? WHERE company_id=?`,
		CompanyDomainStatusPending, time.Now(), id); err != nil {
		return fmt.Errorf("释放公司域名失败: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM companies WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("删除公司失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// ListCompanies 返回全部公司及其已确认域名、资产数。
func (db *DB) ListCompanies() ([]Company, error) {
	rows, err := db.Query(`SELECT id,name,note,created_at,updated_at FROM companies ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("查询公司失败: %w", err)
	}
	var companies []Company
	byID := make(map[string]int)
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Note, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Domains = []CompanyDomain{}
		byID[c.ID] = len(companies)
		companies = append(companies, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(companies) == 0 {
		return companies, nil
	}

	domainRows, err := db.Query(`SELECT id,company_id,domain,source,status,note,created_at,updated_at
		FROM company_domains WHERE company_id IS NOT NULL AND status<>? ORDER BY domain`, CompanyDomainStatusIgnored)
	if err != nil {
		return nil, fmt.Errorf("查询公司域名失败: %w", err)
	}
	allDomains := make([]string, 0)
	for domainRows.Next() {
		var d CompanyDomain
		if err := domainRows.Scan(&d.ID, &d.CompanyID, &d.Domain, &d.Source, &d.Status, &d.Note, &d.CreatedAt, &d.UpdatedAt); err != nil {
			domainRows.Close()
			return nil, err
		}
		idx, ok := byID[d.CompanyID]
		if !ok {
			continue
		}
		companies[idx].Domains = append(companies[idx].Domains, d)
		if d.Status == CompanyDomainStatusConfirmed {
			allDomains = append(allDomains, d.Domain)
		}
	}
	if err := domainRows.Err(); err != nil {
		domainRows.Close()
		return nil, err
	}
	domainRows.Close()

	counts, err := db.AssetCountsByDomain(allDomains)
	if err != nil {
		return nil, err
	}
	for i := range companies {
		for j := range companies[i].Domains {
			c := counts[companies[i].Domains[j].Domain]
			companies[i].Domains[j].AssetCount = c
			companies[i].AssetCount += c
		}
	}
	return companies, nil
}

// GetCompany 按 ID 读取公司基本信息。
func (db *DB) GetCompany(id string) (*Company, error) {
	var c Company
	err := db.QueryRow(`SELECT id,name,note,created_at,updated_at FROM companies WHERE id=?`, strings.TrimSpace(id)).
		Scan(&c.ID, &c.Name, &c.Note, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddCompanyDomain 把主域名归属到公司（手工添加或确认时调用）。
// 已存在同域名时更新归属与状态。
func (db *DB) AddCompanyDomain(companyID, domain, source, note string) (*CompanyDomain, error) {
	domain = normalizeCompanyDomain(domain)
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}
	companyID = strings.TrimSpace(companyID)
	now := time.Now()
	existing, err := db.GetCompanyDomainByDomain(domain)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.CompanyID != "" && existing.CompanyID != companyID && existing.Status == CompanyDomainStatusConfirmed {
		return nil, fmt.Errorf("域名 %s 已归属其他公司", domain)
	}
	if existing != nil {
		if _, err := db.Exec(`UPDATE company_domains SET company_id=?,source=?,status=?,note=?,updated_at=? WHERE id=?`,
			nullIfEmpty(companyID), source, CompanyDomainStatusConfirmed, strings.TrimSpace(note), now, existing.ID); err != nil {
			return nil, fmt.Errorf("更新域名归属失败: %w", err)
		}
		existing.CompanyID = companyID
		existing.Source = source
		existing.Status = CompanyDomainStatusConfirmed
		existing.Note = strings.TrimSpace(note)
		existing.UpdatedAt = now
		return existing, nil
	}
	item := &CompanyDomain{
		ID:        uuid.NewString(),
		CompanyID: companyID,
		Domain:    domain,
		Source:    source,
		Status:    CompanyDomainStatusConfirmed,
		Note:      strings.TrimSpace(note),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := db.Exec(`INSERT INTO company_domains (id,company_id,domain,source,status,note,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		item.ID, nullIfEmpty(companyID), item.Domain, item.Source, item.Status, item.Note, now, now); err != nil {
		return nil, fmt.Errorf("新增公司域名失败: %w", err)
	}
	return item, nil
}

// GetCompanyDomainByDomain 按域名查归属记录。
func (db *DB) GetCompanyDomainByDomain(domain string) (*CompanyDomain, error) {
	domain = normalizeCompanyDomain(domain)
	if domain == "" {
		return nil, nil
	}
	row := db.QueryRow(`SELECT id,COALESCE(company_id,''),domain,source,status,note,created_at,updated_at
		FROM company_domains WHERE domain=?`, domain)
	var d CompanyDomain
	if err := row.Scan(&d.ID, &d.CompanyID, &d.Domain, &d.Source, &d.Status, &d.Note, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// RemoveCompanyDomain 删除一条域名归属记录（域名会重新变成「未跟踪」）。
func (db *DB) RemoveCompanyDomain(companyID, domainID string) error {
	res, err := db.Exec(`DELETE FROM company_domains WHERE id=? AND company_id=?`,
		strings.TrimSpace(domainID), strings.TrimSpace(companyID))
	if err != nil {
		return fmt.Errorf("删除域名归属失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListCompanyDomains 按状态列出域名（status 为空表示全部）。
func (db *DB) ListCompanyDomains(status string) ([]CompanyDomain, error) {
	status = strings.TrimSpace(status)
	query := `SELECT d.id,COALESCE(d.company_id,''),COALESCE(c.name,''),d.domain,d.source,d.status,d.note,d.created_at,d.updated_at
		FROM company_domains d LEFT JOIN companies c ON c.id=d.company_id`
	args := []interface{}{}
	if status != "" {
		query += ` WHERE d.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY d.domain`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询域名列表失败: %w", err)
	}
	defer rows.Close()
	var items []CompanyDomain
	domains := make([]string, 0)
	for rows.Next() {
		var d CompanyDomain
		if err := rows.Scan(&d.ID, &d.CompanyID, &d.CompanyName, &d.Domain, &d.Source, &d.Status, &d.Note, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
		domains = append(domains, d.Domain)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts, err := db.AssetCountsByDomain(domains)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].AssetCount = counts[items[i].Domain]
	}
	return items, nil
}

// SetCompanyDomainStatus 更新域名状态（confirm / ignore 用）。
func (db *DB) SetCompanyDomainStatus(domainID, companyID, status string) error {
	status = strings.TrimSpace(status)
	if status != CompanyDomainStatusConfirmed && status != CompanyDomainStatusPending && status != CompanyDomainStatusIgnored {
		return fmt.Errorf("非法状态: %s", status)
	}
	var companyArg interface{}
	if status == CompanyDomainStatusConfirmed {
		if strings.TrimSpace(companyID) == "" {
			return fmt.Errorf("确认归属时必须指定公司")
		}
		companyArg = strings.TrimSpace(companyID)
	}
	res, err := db.Exec(`UPDATE company_domains SET company_id=?,status=?,updated_at=? WHERE id=?`,
		companyArg, status, time.Now(), strings.TrimSpace(domainID))
	if err != nil {
		return fmt.Errorf("更新域名状态失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpsertPendingDomains 把自动采集发现的根域名登记为「待确认」。
// 已存在的域名（无论什么状态）都不会被改动，避免覆盖用户的手工确认/忽略。
func (db *DB) UpsertPendingDomains(domains []string, source string) (int, error) {
	if len(domains) == 0 {
		return 0, nil
	}
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	inserted := 0
	seen := make(map[string]bool, len(domains))
	for _, raw := range domains {
		domain := normalizeCompanyDomain(raw)
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		res, err := tx.Exec(`INSERT OR IGNORE INTO company_domains
			(id,company_id,domain,source,status,note,created_at,updated_at)
			VALUES (?,NULL,?,?,?,?,?,?)`,
			uuid.NewString(), domain, strings.TrimSpace(source), CompanyDomainStatusPending, "", now, now)
		if err != nil {
			return inserted, fmt.Errorf("登记待确认域名失败: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, tx.Commit()
}

// DomainCompanyMap 返回 domain -> 公司 的映射（仅已确认归属），供层级视图使用。
func (db *DB) DomainCompanyMap() (map[string]Company, error) {
	rows, err := db.Query(`SELECT d.domain,c.id,c.name FROM company_domains d
		JOIN companies c ON c.id=d.company_id WHERE d.status=?`, CompanyDomainStatusConfirmed)
	if err != nil {
		return nil, fmt.Errorf("查询域名归属映射失败: %w", err)
	}
	defer rows.Close()
	result := make(map[string]Company)
	for rows.Next() {
		var domain string
		var c Company
		if err := rows.Scan(&domain, &c.ID, &c.Name); err != nil {
			return nil, err
		}
		result[domain] = c
	}
	return result, rows.Err()
}

// AssetCountsByDomain 统计一批根域名各自的资产数。
func (db *DB) AssetCountsByDomain(domains []string) (map[string]int, error) {
	counts := make(map[string]int, len(domains))
	unique := make([]string, 0, len(domains))
	seen := make(map[string]bool, len(domains))
	for _, d := range domains {
		d = normalizeCompanyDomain(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		unique = append(unique, d)
	}
	if len(unique) == 0 {
		return counts, nil
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
		for _, d := range batch {
			args = append(args, d)
		}
		rows, err := db.Query(`SELECT domain,COUNT(*) FROM assets WHERE domain IN (`+placeholders+`) GROUP BY domain`, args...)
		if err != nil {
			return nil, fmt.Errorf("统计域名资产数失败: %w", err)
		}
		for rows.Next() {
			var domain string
			var n int
			if err := rows.Scan(&domain, &n); err != nil {
				rows.Close()
				return nil, err
			}
			counts[domain] = n
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return counts, nil
}

// LocalDomainCandidates 按公司名在本地数据里找候选主域名：
// 命中项目名、业务系统或部门的资产，按出现次数排序。
func (db *DB) LocalDomainCandidates(companyName string, limit int) (map[string]int, error) {
	name := strings.TrimSpace(companyName)
	if name == "" {
		return map[string]int{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	like := "%" + name + "%"
	rows, err := db.Query(`SELECT a.domain, COUNT(*) AS n FROM assets a
		LEFT JOIN projects p ON p.id=a.project_id
		WHERE a.domain<>'' AND (p.name LIKE ? OR a.business_system LIKE ? OR a.department LIKE ?)
		GROUP BY a.domain ORDER BY n DESC LIMIT ?`, like, like, like, limit)
	if err != nil {
		return nil, fmt.Errorf("查询本地候选域名失败: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var domain string
		var n int
		if err := rows.Scan(&domain, &n); err != nil {
			return nil, err
		}
		result[normalizeCompanyDomain(domain)] = n
	}
	return result, rows.Err()
}
