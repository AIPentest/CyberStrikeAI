package deepassessment

import "testing"

func TestExtractSurfaceKeepsInScopeServicesAndDropsTheRest(t *testing.T) {
	text := `
POST https://app.example.com/login?token=secret
GET /admin
https://a.example.com:8443/v1/users
Found api.example.com
user@example.com
https://other.example.net/admin
http://8.8.8.8/dns
http://203.0.113.10/status
https://b.example.com/x
`
	hits, truncated := ExtractSurface(text, []ScopeSeed{{Domain: "example.com"}, {IP: "203.0.113.10"}})
	if truncated {
		t.Fatal("short output was truncated")
	}
	login := surfaceHit(t, hits, "app.example.com", "", 443, "https")
	if !surfaceHasEndpoint(login, "POST", "/login") || !surfaceHasEndpoint(login, "GET", "/admin") {
		t.Fatalf("login endpoints: %+v", login.Endpoints)
	}
	if surfaceHit(t, hits, "a.example.com", "", 8443, "https") == nil {
		t.Fatal(hits)
	}
	api := surfaceHit(t, hits, "api.example.com", "", 0, "")
	if api == nil {
		t.Fatal("bare in-scope domain was dropped", hits)
	}
	if surfaceHit(t, hits, "", "203.0.113.10", 80, "http") == nil || !surfaceHasEndpoint(surfaceHit(t, hits, "", "203.0.113.10", 80, "http"), "GET", "/status") {
		t.Fatal("seeded address was dropped", hits)
	}
	for _, hit := range hits {
		if hit.Domain == "other.example.net" || hit.IP == "8.8.8.8" || hit.Domain == "example.com" && hit.Port == 0 {
			t.Fatal("out-of-scope or email host recorded", hit)
		}
	}
}

func TestExtractSurfaceDoesNotWidenToSiblingDomains(t *testing.T) {
	hits, _ := ExtractSurface("https://other.example.com/a\nhttps://c.app.example.com/b", []ScopeSeed{{Domain: "app.example.com"}})
	if surfaceHit(t, hits, "c.app.example.com", "", 443, "https") == nil {
		t.Fatal("child domain was dropped", hits)
	}
	for _, hit := range hits {
		if hit.Domain == "other.example.com" {
			t.Fatal("sibling domain recorded")
		}
	}
}

func TestDeclaredTargetsAndExclusions(t *testing.T) {
	declared := ExtractDeclaredTargets("评估 https://app.example.com/login 和 api.example.com，不要把说明文字当目标")
	if surfaceHit(t, declared, "app.example.com", "", 443, "https") == nil || surfaceHit(t, declared, "api.example.com", "", 0, "") == nil {
		t.Fatal(declared)
	}
	excluded := ExcludedTargets("评估 https://app.example.com", "排除 payments.example.com 和 198.51.100.9")
	hits, _ := ExtractSurface("https://app.example.com/a\nhttps://pay.payments.example.com/b\nhttp://198.51.100.9/c\nhttps://api.example.com/d", []ScopeSeed{{Domain: "example.com"}, {IP: "198.51.100.9"}})
	kept := WithoutExcluded(hits, excluded)
	if surfaceHit(t, kept, "app.example.com", "", 443, "https") == nil || surfaceHit(t, kept, "api.example.com", "", 443, "https") == nil {
		t.Fatal("in-scope targets removed", kept)
	}
	for _, hit := range kept {
		if hit.Domain == "pay.payments.example.com" || hit.IP == "198.51.100.9" {
			t.Fatal("excluded target kept", hit)
		}
	}
	if ExcludedTargets("same", "same") != nil {
		t.Fatal("repeated objective was treated as an exclusion")
	}
}

func surfaceHit(t *testing.T, hits []SurfaceHit, domain, ip string, port int, protocol string) *SurfaceHit {
	t.Helper()
	for i := range hits {
		hit := &hits[i]
		if hit.Domain == domain && hit.IP == ip && hit.Port == port && hit.Protocol == protocol {
			return hit
		}
	}
	return nil
}

func surfaceHasEndpoint(hit *SurfaceHit, method, path string) bool {
	if hit == nil {
		return false
	}
	for _, endpoint := range hit.Endpoints {
		if endpoint.Method == method && endpoint.Path == path {
			return true
		}
	}
	return false
}
