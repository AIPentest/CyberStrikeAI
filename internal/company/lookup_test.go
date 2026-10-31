package company

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

func TestRootDomainFromCell(t *testing.T) {
	cases := map[string]string{
		"https://a.b.example.com/path": "example.com",
		"example.com:443":              "example.com",
		"shop.example.co.uk":           "example.co.uk",
		"1.2.3.4":                      "",
		"":                             "",
		"not a domain":                 "",
	}
	for input, want := range cases {
		if got := rootDomainFromCell(input); got != want {
			t.Errorf("rootDomainFromCell(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLooksLikeDomain(t *testing.T) {
	if !looksLikeDomain("example.com") {
		t.Error("example.com 应识别为域名")
	}
	if looksLikeDomain("示例集团") {
		t.Error("公司名不应识别为域名")
	}
	if looksLikeDomain("a b.com") {
		t.Error("含空格不应识别为域名")
	}
}

func TestSuggestFofaAggregatesRootDomains(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("qbase64")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": false,
			"results": [][]interface{}{
				{"a.example.com", "example.com"},
				{"b.example.com", "example.com"},
				{"x.other.com", "other.com"},
				{"1.2.3.4", ""},
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{FOFA: config.FofaConfig{APIKey: "test-key", BaseURL: server.URL}}
	lookuper := NewLookuper(nil, cfg, zap.NewNop())
	suggestions, warnings, err := lookuper.Suggest(context.Background(), "示例集团", 10)
	if err != nil {
		t.Fatalf("Suggest 返回错误: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("不应有警告: %v", warnings)
	}
	if gotQuery == "" {
		t.Fatal("未向 FOFA 传递 qbase64 查询")
	}
	byDomain := make(map[string]Suggestion, len(suggestions))
	for _, item := range suggestions {
		byDomain[item.Domain] = item
	}
	if byDomain["example.com"].Hits != 2 {
		t.Fatalf("example.com 命中次数 = %d, want 2: %+v", byDomain["example.com"].Hits, suggestions)
	}
	if byDomain["example.com"].Source != "fofa" {
		t.Fatalf("source = %q, want fofa", byDomain["example.com"].Source)
	}
	if _, ok := byDomain["other.com"]; !ok {
		t.Fatalf("缺少 other.com: %+v", suggestions)
	}
	// 排序：命中多的在前
	if suggestions[0].Domain != "example.com" {
		t.Fatalf("排序异常: %+v", suggestions)
	}
}

func TestSuggestWithoutFofaKeyWarns(t *testing.T) {
	cfg := &config.Config{} // 未配置 FOFA Key
	lookuper := NewLookuper(nil, cfg, zap.NewNop())
	suggestions, warnings, err := lookuper.Suggest(context.Background(), "示例集团", 10)
	if err != nil {
		t.Fatalf("Suggest 返回错误: %v", err)
	}
	if len(suggestions) != 0 {
		t.Fatalf("无数据源时不应有建议: %+v", suggestions)
	}
	if len(warnings) == 0 {
		t.Fatal("未配置 FOFA Key 时应给出警告")
	}
}

func TestSuggestAcceptsDomainInput(t *testing.T) {
	cfg := &config.Config{}
	lookuper := NewLookuper(nil, cfg, zap.NewNop())
	suggestions, _, err := lookuper.Suggest(context.Background(), "example.com", 10)
	if err != nil {
		t.Fatalf("Suggest 返回错误: %v", err)
	}
	if len(suggestions) != 1 || suggestions[0].Source != "input" {
		t.Fatalf("输入即为域名时应直接作为建议: %+v", suggestions)
	}
}
