package multiagent

import (
	"cyberstrike-ai/internal/config"
	"fmt"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalUserLedgerReservesEntryFormattingBudget(t *testing.T) {
	const scope = "仅授权 ONLY_AUTHORIZED_SCOPE，禁止本机"
	original := scope + " " + strings.Repeat("中文请求详情 ", 1000)
	for _, budget := range []int{64, 200, 1000} {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			messages := []adk.Message{schema.UserMessage(original)}
			body := buildOriginalUserIntentLedger(messages, budget, budget)
			if utf8RuneLen(body) > budget {
				t.Fatalf("ledger exceeds total body budget: %d > %d", utf8RuneLen(body), budget)
			}
			if !strings.HasPrefix(body, "- [U001] "+scope) || !strings.HasSuffix(body, "...") {
				t.Fatalf("bounded human prefix lost: %q", body)
			}
			ledger := buildOriginalUserIntentLedgerMessage(messages, budget, budget)
			if ledger == nil || len(trustedUserIntentLedgerEntries(ledger)) != 1 || !strings.Contains(ledger.Content, scope) {
				t.Fatalf("formatted clipping dropped original human input: %#v", ledger)
			}
		})
	}
}

func TestOriginalUserLedgerPreservesPreparedLongRequest(t *testing.T) {
	cfg := &config.Config{}
	cfg.OpenAI.MaxTotalTokens = 5000
	cfg.Database.Path = filepath.Join(t.TempDir(), "synthetic.db")
	original := "ONLY_AUTHORIZED_SCOPE no localhost\n" + strings.Repeat("request detail ", 1000)
	preview := prepareLatestUserMessageForModel(original, cfg, &config.MultiAgentEinoMiddlewareConfig{}, "audit-human", nil)
	max := modelFacingRuneBudget(5000, .2)
	ledger := buildOriginalUserIntentLedgerMessage([]adk.Message{schema.UserMessage(preview)}, max, max)
	if ledger == nil {
		t.Fatal("long human ledger is missing")
	}
	entries := trustedUserIntentLedgerEntries(ledger)
	if len(entries) != 1 || !strings.Contains(entries[0], "ONLY_AUTHORIZED_SCOPE no localhost") {
		t.Fatalf("prepared request loses raw authorization prefix: %#v", entries)
	}
}

func TestOriginalUserLedgerCapsOverflowNoticesAndTinyBudgets(t *testing.T) {
	msgs := []adk.Message{schema.UserMessage("only authorized.example"), schema.UserMessage(strings.Repeat("additional detail ", 1000))}
	for budget := 1; budget <= 200; budget++ {
		body := buildOriginalUserIntentLedger(msgs, budget, 1000)
		if utf8RuneLen(body) > budget {
			t.Fatalf("budget %d exceeded by %d runes: %q", budget, utf8RuneLen(body), body)
		}
		if strings.Contains(body, "truncated") && !strings.Contains(body, "- [...truncated]") {
			t.Fatalf("incomplete marker at budget %d: %q", budget, body)
		}
		ledger := buildOriginalUserIntentLedgerMessage(msgs, budget, 1000)
		if ledger != nil {
			for _, entry := range trustedUserIntentLedgerEntries(ledger) {
				if strings.Contains(entry, "用户原始输入账本超过预算") || strings.Contains(entry, "truncated") {
					t.Fatalf("notice becomes user input at budget %d: %q", budget, entry)
				}
			}
		}
	}
}

func TestOriginalUserLedgerFormattingKeepsNormalOutput(t *testing.T) {
	msgs := []adk.Message{schema.UserMessage("Only staging, no production."), schema.UserMessage("先验证 /api/login。")}
	want := "- [U001] Only staging, no production.\n- [U002] 先验证 /api/login。"
	if got := buildOriginalUserIntentLedger(msgs, 96000, 16000); got != want {
		t.Fatalf("normal ledger formatting changed: %q", got)
	}
	if got := buildOriginalUserIntentLedger(msgs, 0, 0); got != want {
		t.Fatalf("unbounded ledger formatting changed: %q", got)
	}
}
