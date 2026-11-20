package deepassessment

import (
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/idna"
)

const (
	surfaceTextLimit      = 200000
	surfaceLineLimit      = 4000
	surfaceHostLimit      = 40
	surfaceEndpointLimit  = 200
	surfaceEndpointsOnHit = 50
)

// ScopeSeed is an existing project or objective host. Tool output may add
// names under these hosts; it does not adopt unrelated domains or addresses.
type ScopeSeed struct {
	Host   string
	IP     string
	Domain string
}

// SurfaceEndpoint is an HTTP path observed on a host, without query or fragment.
type SurfaceEndpoint struct {
	Method string
	Path   string
}

// SurfaceHit is one service to record on the shared attack-surface graph.
type SurfaceHit struct {
	IP        string
	Domain    string
	Protocol  string
	Port      int
	Endpoints []SurfaceEndpoint
}

var (
	surfaceURLPattern    = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"'\\\]]+`)
	surfaceHostPattern   = regexp.MustCompile(`(?i)\b((?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}|(?:\d{1,3}\.){3}\d{1,3}):(\d{1,5})\b`)
	surfaceDomainPattern = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b`)
	surfaceIPPattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	surfacePathPattern   = regexp.MustCompile(`(?i)^\s*(?:(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+)?(/[^\s?#]*)\s*$`)
)

var surfaceMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true,
}

type surfaceScope struct {
	allowAll bool
	ips      map[string]bool
	domains  []string
}

func newSurfaceScope(seeds []ScopeSeed, allowAll bool) surfaceScope {
	scope := surfaceScope{allowAll: allowAll, ips: map[string]bool{}}
	seen := map[string]bool{}
	add := func(ip, domain string) {
		if ip != "" {
			scope.ips[ip] = true
		}
		if domain != "" && !seen[domain] {
			seen[domain] = true
			scope.domains = append(scope.domains, domain)
		}
	}
	for _, seed := range seeds {
		for _, raw := range []string{seed.Host, seed.IP, seed.Domain} {
			ip, domain, ok := classifySurfaceHost(raw)
			if ok {
				add(ip, domain)
			}
		}
	}
	return scope
}

func (s surfaceScope) allows(raw string) (ip, domain string, ok bool) {
	ip, domain, ok = classifySurfaceHost(raw)
	if !ok {
		return "", "", false
	}
	if s.allowAll {
		return ip, domain, true
	}
	if ip != "" {
		return ip, "", s.ips[ip]
	}
	for _, root := range s.domains {
		if domain == root || strings.HasSuffix(domain, "."+root) {
			return "", domain, true
		}
	}
	return "", "", false
}

func classifySurfaceHost(raw string) (ip, domain string, ok bool) {
	host := strings.TrimSpace(raw)
	if host == "" {
		return "", "", false
	}
	if strings.Contains(host, "://") {
		parsed, err := url.Parse(host)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil {
			return "", "", false
		}
		host = parsed.Hostname()
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	host = strings.TrimSuffix(host, ".")
	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.String(), "", true
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", "", false
	}
	ascii = strings.ToLower(strings.TrimSuffix(ascii, "."))
	if ascii == "localhost" || validSurfaceDomain(ascii) {
		return "", ascii, true
	}
	return "", "", false
}

func validSurfaceDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.Contains(domain, "..") || net.ParseIP(domain) != nil {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// ExtractSurface keeps hosts and HTTP paths from tool output when they belong
// to seeds. Unrelated addresses are ignored. The result is capped per call.
func ExtractSurface(text string, seeds []ScopeSeed) ([]SurfaceHit, bool) {
	return extractSurface(text, newSurfaceScope(seeds, false))
}

// ExtractDeclaredTargets reads hosts the user wrote into the objective.
// Supplementary exclusions are not declared targets.
func ExtractDeclaredTargets(text string) []SurfaceHit {
	hits, _ := extractSurface(text, newSurfaceScope(nil, true))
	return hits
}

// ExcludedTargets reads hosts named in a real exclusion note. A scope that
// only repeats the objective does not exclude those targets.
func ExcludedTargets(objective, scope string) []ScopeSeed {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(scope) == strings.TrimSpace(objective) {
		return nil
	}
	return SeedsFromHits(ExtractDeclaredTargets(scope))
}

// SeedsFromHits turns recorded services back into scope names.
func SeedsFromHits(hits []SurfaceHit) []ScopeSeed {
	out := make([]ScopeSeed, 0, len(hits))
	for _, hit := range hits {
		out = append(out, ScopeSeed{IP: hit.IP, Domain: hit.Domain})
	}
	return out
}

// WithoutExcluded removes services the user named in the exclusion note,
// including their subdomains.
func WithoutExcluded(hits []SurfaceHit, excluded []ScopeSeed) []SurfaceHit {
	if len(excluded) == 0 {
		return hits
	}
	block := newSurfaceScope(excluded, false)
	out := make([]SurfaceHit, 0, len(hits))
	for _, hit := range hits {
		name := hit.Domain
		if name == "" {
			name = hit.IP
		}
		if _, _, ok := block.allows(name); ok {
			continue
		}
		out = append(out, hit)
	}
	return out
}

type surfaceBuilder struct {
	scope     surfaceScope
	hits      map[string]*SurfaceHit
	endpoints int
	truncated bool
}

func extractSurface(text string, scope surfaceScope) ([]SurfaceHit, bool) {
	if len(text) > surfaceTextLimit {
		text = text[:surfaceTextLimit]
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	builder := &surfaceBuilder{scope: scope, hits: map[string]*SurfaceHit{}}
	if len(lines) > surfaceLineLimit {
		lines = lines[:surfaceLineLimit]
		builder.truncated = true
	}
	var carried *SurfaceHit
	for _, line := range lines {
		if len(line) > 4000 {
			line = line[:4000]
			builder.truncated = true
		}
		hosts := builder.hostsOnLine(line)
		switch len(hosts) {
		case 1:
			carried = hosts[0]
		case 0:
			if carried != nil {
				if method, path, ok := loneSurfacePath(line); ok {
					builder.addEndpoint(carried, method, path)
				}
			}
		default:
			carried = nil
		}
	}
	return builder.finish(), builder.truncated
}

func (b *surfaceBuilder) hostsOnLine(line string) []*SurfaceHit {
	urls := surfaceURLPattern.FindAllString(line, 20)
	stripped := surfaceURLPattern.ReplaceAllString(line, " ")
	var hosts []*SurfaceHit
	seen := map[string]bool{}
	remember := func(hit *SurfaceHit) {
		if hit == nil || seen[hit.key()] {
			return
		}
		seen[hit.key()] = true
		hosts = append(hosts, hit)
	}
	method := surfaceLineMethod(line)
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil || parsed.Hostname() == "" {
			continue
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}
		ip, domain, ok := b.scope.allows(parsed.Hostname())
		if !ok {
			continue
		}
		port := surfacePort(parsed)
		hit := b.add(ip, domain, scheme, port)
		remember(hit)
		if path := cleanSurfacePath(parsed.EscapedPath()); path != "" && path != "/" {
			b.addEndpoint(hit, method, path)
		}
	}
	for _, match := range surfaceHostPattern.FindAllStringSubmatch(stripped, 20) {
		ip, domain, ok := b.scope.allows(match[1])
		if !ok {
			continue
		}
		port := atoiPort(match[2])
		if port <= 0 || port > 65535 {
			continue
		}
		protocol := ""
		if port == 443 {
			protocol = "https"
		} else if port == 80 {
			protocol = "http"
		}
		remember(b.add(ip, domain, protocol, port))
	}
	for _, match := range surfaceDomainPattern.FindAllString(stripped, 20) {
		if skippedEmail(stripped, match) {
			continue
		}
		_, domain, ok := b.scope.allows(match)
		if !ok {
			continue
		}
		remember(b.add("", domain, "", 0))
	}
	for _, match := range surfaceIPPattern.FindAllString(stripped, 20) {
		ip, _, ok := b.scope.allows(match)
		if !ok {
			continue
		}
		remember(b.add(ip, "", "", 0))
	}
	if len(hosts) == 1 {
		if method, path, ok := loneSurfacePath(line); ok {
			b.addEndpoint(hosts[0], method, path)
		}
	}
	return hosts
}

func skippedEmail(line, match string) bool {
	at := strings.Index(strings.ToLower(line), strings.ToLower(match))
	return at > 0 && line[at-1] == '@'
}

func (b *surfaceBuilder) add(ip, domain, protocol string, port int) *SurfaceHit {
	if ip == "" && domain == "" {
		return nil
	}
	key := surfaceKey(ip, domain, protocol, port)
	if hit := b.hits[key]; hit != nil {
		return hit
	}
	if len(b.hits) >= surfaceHostLimit {
		b.truncated = true
		return nil
	}
	hit := &SurfaceHit{IP: ip, Domain: domain, Protocol: protocol, Port: port}
	b.hits[key] = hit
	return hit
}

func (b *surfaceBuilder) addEndpoint(hit *SurfaceHit, method, path string) {
	if hit == nil {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if !surfaceMethods[method] {
		method = "GET"
	}
	path = cleanSurfacePath(path)
	if path == "" || path == "/" {
		return
	}
	for _, existing := range hit.Endpoints {
		if existing.Method == method && existing.Path == path {
			return
		}
	}
	if len(hit.Endpoints) >= surfaceEndpointsOnHit || b.endpoints >= surfaceEndpointLimit {
		b.truncated = true
		return
	}
	hit.Endpoints = append(hit.Endpoints, SurfaceEndpoint{Method: method, Path: path})
	b.endpoints++
}

func (b *surfaceBuilder) finish() []SurfaceHit {
	specific := map[string][]*SurfaceHit{}
	for _, hit := range b.hits {
		if hit.Port != 0 || hit.Protocol != "" {
			key := hit.Domain + "|" + hit.IP
			specific[key] = append(specific[key], hit)
		}
	}
	for _, hit := range b.hits {
		if hit.Port != 0 || hit.Protocol != "" {
			continue
		}
		targets := specific[hit.Domain+"|"+hit.IP]
		if len(targets) == 0 {
			continue
		}
		for _, endpoint := range hit.Endpoints {
			for _, target := range targets {
				appendSurfaceEndpoint(target, endpoint)
			}
		}
		delete(b.hits, hit.key())
	}
	out := make([]SurfaceHit, 0, len(b.hits))
	for _, hit := range b.hits {
		sort.Slice(hit.Endpoints, func(i, j int) bool {
			if hit.Endpoints[i].Path == hit.Endpoints[j].Path {
				return hit.Endpoints[i].Method < hit.Endpoints[j].Method
			}
			return hit.Endpoints[i].Path < hit.Endpoints[j].Path
		})
		out = append(out, *hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

func appendSurfaceEndpoint(hit *SurfaceHit, endpoint SurfaceEndpoint) {
	for _, existing := range hit.Endpoints {
		if existing.Method == endpoint.Method && existing.Path == endpoint.Path {
			return
		}
	}
	hit.Endpoints = append(hit.Endpoints, endpoint)
}

func (h SurfaceHit) key() string {
	return surfaceKey(h.IP, h.Domain, h.Protocol, h.Port)
}

func surfaceKey(ip, domain, protocol string, port int) string {
	return domain + "|" + ip + "|" + itoaPort(port) + "|" + protocol
}

func surfacePort(parsed *url.URL) int {
	if parsed.Port() != "" {
		if port := atoiPort(parsed.Port()); port > 0 && port <= 65535 {
			return port
		}
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return 443
	}
	if strings.EqualFold(parsed.Scheme, "http") {
		return 80
	}
	return 0
}

func surfaceLineMethod(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "GET"
	}
	method := strings.ToUpper(strings.Trim(fields[0], "[]"))
	if surfaceMethods[method] {
		return method
	}
	return "GET"
}

func loneSurfacePath(line string) (method, path string, ok bool) {
	match := surfacePathPattern.FindStringSubmatch(line)
	if match == nil {
		return "", "", false
	}
	method = strings.ToUpper(match[1])
	if method == "" {
		method = "GET"
	}
	path = cleanSurfacePath(match[2])
	if path == "" || path == "/" {
		return "", "", false
	}
	return method, path, true
}

func cleanSurfacePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
		raw = raw[:cut]
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, " \r\n\x00") || len(raw) > 2048 {
		return ""
	}
	return raw
}

func atoiPort(raw string) int {
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 65535 {
			return 0
		}
	}
	return n
}

func itoaPort(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [5]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
