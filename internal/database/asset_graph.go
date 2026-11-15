package database

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"golang.org/x/net/publicsuffix"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AssetGraph is a permission-filtered projection of the shared asset library.
// Graph identities never contain assessment IDs; a task only holds references.
type AssetGraphNode struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	State    string   `json:"state"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	AssetIDs []string `json:"asset_ids"`
}
type AssetGraphEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}
type AssetGraph struct {
	Coverage   map[string]int   `json:"coverage,omitempty"`
	Nodes      []AssetGraphNode `json:"nodes"`
	Edges      []AssetGraphEdge `json:"edges"`
	Assets     []*Asset         `json:"assets"`
	Total      int              `json:"total"`
	NextOffset int              `json:"next_offset"`
	HasMore    bool             `json:"has_more"`
}
type AssetEndpoint struct {
	ID      string `json:"id"`
	AssetID string `json:"asset_id"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Title   string `json:"title"`
}

func (db *DB) PutAssetEndpoint(e *AssetEndpoint, access RBACListAccess) error {
	if _, err := db.GetAsset(e.AssetID, access); err != nil {
		return err
	}
	e.Method = strings.ToUpper(strings.TrimSpace(e.Method))
	if !map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true}[e.Method] {
		return fmt.Errorf("invalid HTTP method")
	}
	u, err := url.Parse(e.Path)
	if err != nil || !strings.HasPrefix(e.Path, "/") || strings.HasPrefix(e.Path, "//") || u.IsAbs() || u.RawQuery != "" || u.Fragment != "" || len(e.Path) > 2048 || len(e.Title) > 500 || strings.ContainsAny(e.Path, "\r\n\x00") {
		return fmt.Errorf("endpoint path must be relative to the asset, without query values or fragments")
	}
	e.ID = fmt.Sprintf("endpoint:%x", sha256.Sum256([]byte(e.AssetID+"\n"+e.Method+"\n"+e.Path)))
	now := time.Now().UTC()
	_, err = db.Exec("INSERT INTO asset_endpoints(id,asset_id,method,path,title,first_seen_at,last_seen_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(asset_id,method,path) DO UPDATE SET title=excluded.title,last_seen_at=excluded.last_seen_at", e.ID, e.AssetID, e.Method, e.Path, e.Title, now, now)
	return err
}
func (db *DB) AssetEndpoints(asset string, offset int, access RBACListAccess) ([]AssetEndpoint, error) {
	if _, err := db.GetAsset(asset, access); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := db.Query("SELECT id,asset_id,method,path,title FROM asset_endpoints WHERE asset_id=? ORDER BY id LIMIT 50 OFFSET ?", asset, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetEndpoint{}
	for rows.Next() {
		var e AssetEndpoint
		if err = rows.Scan(&e.ID, &e.AssetID, &e.Method, &e.Path, &e.Title); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (db *DB) SharedAssetGraph(filter AssetListFilter, access RBACListAccess, limit, offset int) (*AssetGraph, error) {
	if limit < 1 || limit > 20 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	assets, total, err := db.ListAssets(limit, offset, filter, access)
	if err != nil {
		return nil, err
	}
	g := &AssetGraph{Nodes: []AssetGraphNode{}, Edges: []AssetGraphEdge{}, Assets: assets, Total: total, NextOffset: offset + len(assets), HasMore: offset+len(assets) < total}
	nodes := map[string]*AssetGraphNode{}
	edges := map[string]bool{}
	add := func(id, kind, title, body, asset string) string {
		if n := nodes[id]; n != nil {
			found := false
			for _, v := range n.AssetIDs {
				found = found || v == asset
			}
			if !found {
				n.AssetIDs = append(n.AssetIDs, asset)
			}
		} else {
			nodes[id] = &AssetGraphNode{ID: id, Kind: kind, Title: title, Body: body, State: "inventory", AssetIDs: []string{asset}}
		}
		return id
	}
	edge := func(a, b, r string) {
		if a == "" || b == "" || a == b {
			return
		}
		k := a + "\n" + b + "\n" + r
		if !edges[k] {
			g.Edges = append(g.Edges, AssetGraphEdge{a, b, r})
			edges[k] = true
		}
	}
	for _, a := range assets {
		raw, _ := json.Marshal(a)
		title := a.Host
		if title == "" {
			title = a.Domain
		}
		if title == "" {
			title = a.IP
		}
		service := add("asset:"+a.ID, "service", title, string(raw), a.ID)
		domain := ""
		if a.Domain != "" {
			kind := "domain"
			root, err := publicsuffix.EffectiveTLDPlusOne(a.Domain)
			if err == nil && root != a.Domain {
				kind = "subdomain"
				add("domain:"+root, "domain", root, "", a.ID)
				edge("domain:"+root, "domain:"+a.Domain, "contains")
			}
			domain = add("domain:"+a.Domain, kind, a.Domain, "", a.ID)
			edge(domain, service, "exposes")
		}
		if a.IP != "" {
			ip := add("ip:"+a.IP, "ip", a.IP, "", a.ID)
			edge(domain, ip, "associated_with")
			edge(ip, service, "associated_with")
		}
		if a.BusinessSystem != "" {
			id := fmt.Sprintf("application:%x", sha256.Sum256([]byte(a.ID+"\n"+a.BusinessSystem)))
			app := add(id, "application", a.BusinessSystem, "", a.ID)
			edge(service, app, "serves")
		}
		endpoints, err := db.AssetEndpoints(a.ID, 0, access)
		if err != nil {
			return nil, err
		}
		for _, ep := range endpoints {
			raw, _ := json.Marshal(ep)
			id := add(ep.ID, "endpoint", ep.Method+" "+ep.Path, string(raw), a.ID)
			edge(service, id, "contains")
		}
	}
	ids := []string{}
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g.Nodes = append(g.Nodes, *nodes[id])
	}
	return g, nil
}
