package project

import (
	"strings"
	"testing"
)

func TestRequiresAttackChainBody(t *testing.T) {
	cases := []struct {
		cat, key string
		want     bool
	}{
		{"finding", "note/misc", true},
		{"note", "finding/sqli-login", true},
		{"target", "target/primary_domain", false},
		{"auth", "auth/admin_cookie", false},
		{"chain", "x", true},
		{"", "exploit/rce-upload", true},
	}
	for _, tc := range cases {
		if got := RequiresAttackChainBody(tc.cat, tc.key); got != tc.want {
			t.Errorf("RequiresAttackChainBody(%q,%q)=%v want %v", tc.cat, tc.key, got, tc.want)
		}
	}
}

func TestIsSparseFactBody(t *testing.T) {
	long := strings.Repeat("x", 150)
	if !IsSparseFactBody("finding", "finding/x", "") {
		t.Error("empty body should be sparse")
	}
	if !IsSparseFactBody("finding", "finding/x", long) {
		t.Error("body without repro clues should be sparse")
	}
	body := "## Attack chain\n1. step\n## Exploit\n```http\nGET / HTTP/1.1\n```\n"
	if IsSparseFactBody("finding", "finding/x", body) {
		t.Error("structured body should not be sparse")
	}
	if IsSparseFactBody("target", "target/x", "") {
		t.Error("env fact empty body is ok")
	}
}
