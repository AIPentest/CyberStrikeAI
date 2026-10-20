package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureLocalConfigCreatesFromExample(t *testing.T) {
	dir := t.TempDir()
	examplePath := filepath.Join(dir, "config.example.yaml")
	configPath := filepath.Join(dir, "config.yaml")

	example := []byte(`auth:
  session_duration_hours: 12
server:
  host: 127.0.0.1
  port: 8080
`)
	if err := os.WriteFile(examplePath, example, 0644); err != nil {
		t.Fatalf("write example: %v", err)
	}

	result, err := EnsureLocalConfig(configPath)
	if err != nil {
		t.Fatalf("EnsureLocalConfig: %v", err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true")
	}
	if result.ExamplePath != examplePath {
		t.Fatalf("ExamplePath = %q, want %q", result.ExamplePath, examplePath)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load generated config: %v", err)
	}
	if cfg.Auth.SessionDurationHours != 12 {
		t.Fatalf("SessionDurationHours = %d, want 12", cfg.Auth.SessionDurationHours)
	}

	second, err := EnsureLocalConfig(configPath)
	if err != nil {
		t.Fatalf("EnsureLocalConfig existing: %v", err)
	}
	if second.Created {
		t.Fatal("Created = true for existing config, want false")
	}
}

func TestLoadIgnoresLegacyAuthPasswordField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := strings.Join([]string{
		"auth:",
		`  password: "legacy-password"`,
		"  session_duration_hours: 12",
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.SessionDurationHours != 12 {
		t.Fatalf("SessionDurationHours = %d, want 12", cfg.Auth.SessionDurationHours)
	}
}

func TestHitlEffectiveAuditBackend(t *testing.T) {
	if got := (HitlConfig{}).EffectiveAuditBackend(); got != HitlAuditBackendOpenAI {
		t.Fatalf("empty backend = %q, want openai", got)
	}
	if got := (HitlConfig{AuditBackend: "Jev"}).EffectiveAuditBackend(); got != HitlAuditBackendTypeSafe {
		t.Fatalf("jev alias = %q, want typesafe", got)
	}
	if got := (HitlConfig{AuditBackend: "claude"}).EffectiveAuditBackend(); got != HitlAuditBackendOpenAI {
		t.Fatalf("unknown backend = %q, want openai", got)
	}
}

func TestHitlTypeSafeConfigEffectiveDoesNotInheritMainKey(t *testing.T) {
	gotURL, gotKey, gotModel := (HitlConfig{
		AuditBackend: "typesafe",
		AuditModel:   OpenAIConfig{APIKey: "ts-key"},
	}).TypeSafeConfigEffective()
	if gotURL != TypeSafeDefaultBaseURL {
		t.Fatalf("base url = %q, want default", gotURL)
	}
	if gotKey != "ts-key" {
		t.Fatalf("api key = %q, want ts-key", gotKey)
	}
	if gotModel != TypeSafeDefaultModel {
		t.Fatalf("model = %q, want default", gotModel)
	}
}

func TestHitlAuditModelEffectiveFallsBackToMainConfig(t *testing.T) {
	main := OpenAIConfig{
		Provider: "openai",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "main-key",
		Model:    "large-model",
	}

	got := (HitlConfig{
		AuditModel: OpenAIConfig{Model: "small-reviewer"},
	}).AuditModelEffective(main)

	if got.Provider != main.Provider || got.BaseURL != main.BaseURL || got.APIKey != main.APIKey {
		t.Fatalf("expected provider/base_url/api_key to inherit main config, got %+v", got)
	}
	if got.Model != "small-reviewer" {
		t.Fatalf("expected audit model override, got %q", got.Model)
	}
}

func TestHitlDefaultConfigEffectiveValues(t *testing.T) {
	if got := (HitlConfig{}).EffectiveDefaultMode(); got != "off" {
		t.Fatalf("empty default mode = %q, want off", got)
	}
	if got := (HitlConfig{DefaultMode: "review-edit"}).EffectiveDefaultMode(); got != "off" {
		t.Fatalf("unknown default mode = %q, want off", got)
	}
	if got := (HitlConfig{DefaultMode: "review_edit"}).EffectiveDefaultMode(); got != "review_edit" {
		t.Fatalf("review_edit default mode = %q, want review_edit", got)
	}
	if got := (HitlConfig{}).EffectiveDefaultTimeoutSeconds(); got != 300 {
		t.Fatalf("empty default timeout = %d, want 300", got)
	}
	zero := 0
	if got := (HitlConfig{DefaultTimeoutSeconds: &zero}).EffectiveDefaultTimeoutSeconds(); got != 0 {
		t.Fatalf("zero default timeout = %d, want 0", got)
	}
	neg := -1
	if got := (HitlConfig{DefaultTimeoutSeconds: &neg}).EffectiveDefaultTimeoutSeconds(); got != 0 {
		t.Fatalf("negative default timeout = %d, want 0", got)
	}
}

func TestLoadUsesAIDefaultChannelAsRuntimeOpenAI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := strings.Join([]string{
		"ai:",
		"  default_channel: deepseek",
		"  channels:",
		"    qwen:",
		"      name: Qwen",
		"      provider: openai_compatible",
		"      base_url: https://dashscope.example/v1",
		"      api_key: qwen-key",
		"      model: qwen-max",
		"    deepseek:",
		"      name: DeepSeek",
		"      provider: openai_compatible",
		"      base_url: https://deepseek.example/v1",
		"      api_key: deepseek-key",
		"      model: deepseek-chat",
		"      max_total_tokens: 64000",
		"      context_window: 131072",
		"      context_profile: local",
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OpenAI.Model != "deepseek-chat" || cfg.OpenAI.APIKey != "deepseek-key" || cfg.OpenAI.MaxTotalTokens != 64000 {
		t.Fatalf("runtime OpenAI config did not follow ai.default_channel: %+v", cfg.OpenAI)
	}
	if cfg.OpenAI.ContextWindow != 131072 || cfg.OpenAI.ContextProfile != "local" {
		t.Fatalf("context profile fields not loaded: %+v", cfg.OpenAI)
	}
	oa, id, ok := cfg.ResolveAIChannel("qwen")
	if !ok || id != "qwen" || oa.Model != "qwen-max" || oa.APIKey != "qwen-key" {
		t.Fatalf("ResolveAIChannel(qwen) = (%+v, %q, %v)", oa, id, ok)
	}
}

func TestLoadRejectsInvalidChannelContextSettings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		profile string
		window  string
		wantErr string
	}{
		{name: "unknown profile", profile: "turbo", wantErr: "context_profile"},
		{name: "negative window", profile: "local", window: "-1", wantErr: "context_window"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			lines := []string{
				"ai:",
				"  default_channel: qwen",
				"  channels:",
				"    qwen:",
				"      base_url: https://example.invalid/v1",
				"      api_key: key",
				"      model: qwen-max",
			}
			if tc.profile != "" {
				lines = append(lines, "      context_profile: "+tc.profile)
			}
			if tc.window != "" {
				lines = append(lines, "      context_window: "+tc.window)
			}
			lines = append(lines, "")
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeAIProviderProfilesForOfficialDeepSeekEndpoint(t *testing.T) {
	cfg := &Config{
		OpenAI: OpenAIConfig{
			BaseURL: "https://api.deepseek.com/v1",
			Model:   "deepseek-chat",
			Reasoning: OpenAIReasoningConfig{
				Profile: "openai_compat",
			},
		},
		AI: AIConfig{
			Channels: map[string]AIChannelConfig{
				"official": {
					BaseURL: "api.deepseek.com/v1",
					Model:   "deepseek-chat",
					Reasoning: OpenAIReasoningConfig{
						Profile: "auto",
					},
				},
				"gateway": {
					BaseURL: "https://compatible.example.com/v1",
					Model:   "deepseek-chat",
					Reasoning: OpenAIReasoningConfig{
						Profile: "openai_compat",
					},
				},
			},
		},
	}

	cfg.NormalizeAIProviderProfiles()

	if cfg.OpenAI.Reasoning.Profile != "deepseek" {
		t.Fatalf("openai profile = %q, want deepseek", cfg.OpenAI.Reasoning.Profile)
	}
	if got := cfg.AI.Channels["official"].Reasoning.Profile; got != "deepseek" {
		t.Fatalf("official channel profile = %q, want deepseek", got)
	}
	if got := cfg.AI.Channels["gateway"].Reasoning.Profile; got != "openai_compat" {
		t.Fatalf("gateway profile should be preserved, got %q", got)
	}
}

func TestLoadNormalizesDefaultChannelForOfficialDeepSeekEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := strings.Join([]string{
		"ai:",
		"  default_channel: deepseek",
		"  channels:",
		"    deepseek:",
		"      name: DeepSeek",
		"      provider: openai_compatible",
		"      base_url: https://api.deepseek.com/v1",
		"      api_key: deepseek-key",
		"      model: deepseek-chat",
		"      reasoning:",
		"        profile: openai_compat",
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OpenAI.Reasoning.Profile != "deepseek" {
		t.Fatalf("runtime OpenAI profile = %q, want deepseek", cfg.OpenAI.Reasoning.Profile)
	}
	if got := cfg.AI.Channels["deepseek"].Reasoning.Profile; got != "deepseek" {
		t.Fatalf("channel profile = %q, want deepseek", got)
	}
}

func TestSummarizationUserIntentLedgerRunesEffective(t *testing.T) {
	var zero MultiAgentEinoMiddlewareConfig
	if got := zero.SummarizationUserIntentLedgerMaxRunesEffective(); got != DefaultSummarizationUserIntentLedgerMaxRunes {
		t.Fatalf("default ledger max runes = %d, want %d", got, DefaultSummarizationUserIntentLedgerMaxRunes)
	}
	if got := zero.SummarizationUserIntentLedgerEntryMaxRunesEffective(); got != DefaultSummarizationUserIntentLedgerEntryMaxRunes {
		t.Fatalf("default ledger entry max runes = %d, want %d", got, DefaultSummarizationUserIntentLedgerEntryMaxRunes)
	}

	custom := MultiAgentEinoMiddlewareConfig{
		SummarizationUserIntentLedgerMaxRunes:      12345,
		SummarizationUserIntentLedgerEntryMaxRunes: 2345,
	}
	if got := custom.SummarizationUserIntentLedgerMaxRunesEffective(); got != 12345 {
		t.Fatalf("custom ledger max runes = %d", got)
	}
	if got := custom.SummarizationUserIntentLedgerEntryMaxRunesEffective(); got != 2345 {
		t.Fatalf("custom ledger entry max runes = %d", got)
	}
}

func TestSummarizationOutputReserveTokensEffective(t *testing.T) {
	var zero MultiAgentEinoMiddlewareConfig
	if got := zero.SummarizationOutputReserveTokensEffective(); got != DefaultSummarizationOutputReserveTokens {
		t.Fatalf("default output reserve = %d, want %d", got, DefaultSummarizationOutputReserveTokens)
	}
	custom := MultiAgentEinoMiddlewareConfig{SummarizationOutputReserveTokens: 4096}
	if got := custom.SummarizationOutputReserveTokensEffective(); got != 4096 {
		t.Fatalf("custom output reserve = %d", got)
	}
}

func TestOpenAIOutputLimitValidation(t *testing.T) {
	if got := (OpenAIConfig{}).MaxCompletionTokensEffective(); got != DefaultMaxCompletionTokens {
		t.Fatalf("max completion default=%d", got)
	}
	if err := validateOpenAIOutputLimits(OpenAIConfig{MaxCompletionTokens: -1}); err == nil {
		t.Fatal("negative completion limit must fail")
	}
}

func TestLatestUserMessageRunesEffective(t *testing.T) {
	var zero MultiAgentEinoMiddlewareConfig
	if got := zero.LatestUserMessageMaxRunesEffective(); got != DefaultLatestUserMessageMaxRunes {
		t.Fatalf("default latest user max runes = %d, want %d", got, DefaultLatestUserMessageMaxRunes)
	}
	if got := zero.LatestUserMessageHeadRunesEffective(); got != DefaultLatestUserMessageHeadRunes {
		t.Fatalf("default latest user head runes = %d, want %d", got, DefaultLatestUserMessageHeadRunes)
	}
	if got := zero.LatestUserMessageTailRunesEffective(); got != DefaultLatestUserMessageTailRunes {
		t.Fatalf("default latest user tail runes = %d, want %d", got, DefaultLatestUserMessageTailRunes)
	}

	custom := MultiAgentEinoMiddlewareConfig{
		LatestUserMessageMaxRunes:  100,
		LatestUserMessageHeadRunes: 40,
		LatestUserMessageTailRunes: 60,
	}
	if got := custom.LatestUserMessageMaxRunesEffective(); got != 100 {
		t.Fatalf("custom latest user max runes = %d", got)
	}
	if got := custom.LatestUserMessageHeadRunesEffective(); got != 40 {
		t.Fatalf("custom latest user head runes = %d", got)
	}
	if got := custom.LatestUserMessageTailRunesEffective(); got != 60 {
		t.Fatalf("custom latest user tail runes = %d", got)
	}
}

func TestLoadUpdateRepoReadsOnlyTheSourceField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// The rest of the file may be broken or unvalidatable; the CLI only asks for the
	// update source, and a full config load must not be a precondition for updating.
	if err := os.WriteFile(path, []byte("update:\n  repo: https://github.com/Sycun/CyberStrikeAI.git\nserver:\n  port: 8088\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := LoadUpdateRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	if repo != "https://github.com/Sycun/CyberStrikeAI.git" {
		t.Fatalf("repo = %q, want the configured address", repo)
	}

	// Unset and missing files both mean "the official repository is the default".
	if err := os.WriteFile(path, []byte("server:\n  port: 8088\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if repo, err := LoadUpdateRepo(path); err != nil || repo != "" {
		t.Fatalf("repo = %q err = %v, want empty with no error", repo, err)
	}
	if repo, err := LoadUpdateRepo(filepath.Join(dir, "missing.yaml")); err != nil || repo != "" {
		t.Fatalf("a missing config is not an error: repo = %q err = %v", repo, err)
	}

	// A file that is not YAML at all must fail loudly rather than silently fall back to
	// the official repository (that would be an update from the wrong place).
	if err := os.WriteFile(path, []byte("update: [not: a: map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUpdateRepo(path); err == nil {
		t.Fatal("a malformed config must be an error, not a silent default")
	}
}

func TestFileVersionAndWriteVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Missing file reads as "unknown", not an error.
	if v, err := FileVersion(filepath.Join(dir, "missing.yaml")); err != nil || v != "" {
		t.Fatalf("missing file: %q %v", v, err)
	}

	original := "version: \"v1.0.0\"\nserver:\n  port: 8088\n  # a nested word: version: should stay put\nnotes:\n  version: nested-untouched\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := FileVersion(path); err != nil || v != "v1.0.0" {
		t.Fatalf("version = %q err = %v", v, err)
	}

	changed, err := WriteVersion(path, "v1.1.0")
	if err != nil || !changed {
		t.Fatalf("write: changed=%v err=%v", changed, err)
	}
	got := readTestFile(t, path)
	want := "version: \"v1.1.0\"\nserver:\n  port: 8088\n  # a nested word: version: should stay put\nnotes:\n  version: nested-untouched\n"
	if got != want {
		t.Fatalf("file after write:\n%q\nwant:\n%q", got, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatalf("mode = %v err = %v, want 0640 preserved", info.Mode(), err)
	}

	// Idempotent: the same value is not rewritten.
	if changed, err := WriteVersion(path, "v1.1.0"); err != nil || changed {
		t.Fatalf("rewriting the same value: changed=%v err=%v", changed, err)
	}

	// No top-level field: insert at the top, nested keys stay untouched.
	if err := os.WriteFile(path, []byte("server:\n  port: 8088\nnotes:\n  version: nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := WriteVersion(path, "v1.2.0"); err != nil || !changed {
		t.Fatalf("insert: changed=%v err=%v", changed, err)
	}
	got = readTestFile(t, path)
	if !strings.HasPrefix(got, "version: \"v1.2.0\"\n") || !strings.Contains(got, "  version: nested") {
		t.Fatalf("file after insert:\n%q", got)
	}

	// A value that would write malformed YAML is refused rather than written.
	if _, err := WriteVersion(path, "v1.0.0\"\nserver: hijacked"); err == nil {
		t.Fatal("a version containing a quote/newline must be refused")
	}
}

func TestWriteVersionPreservesYAMLDocumentStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := "---\nserver:\n  port: 8088\n"
	const originalMode = os.FileMode(0o600)
	if err := os.WriteFile(path, []byte(original), originalMode); err != nil {
		t.Fatal(err)
	}

	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := WriteVersion(path, "v1.2.3")
	if err != nil || !changed {
		t.Fatalf("WriteVersion: changed=%v err=%v", changed, err)
	}
	got := readTestFile(t, path)
	want := "---\nversion: \"v1.2.3\"\nserver:\n  port: 8088\n"
	if got != want {
		t.Fatalf("file after write:\n%q\nwant:\n%q", got, want)
	}
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("YAML document start must remain first: %q", got)
	}
	if version, err := FileVersion(path); err != nil || version != "v1.2.3" {
		t.Fatalf("FileVersion = %q, %v; want v1.2.3", version, err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after WriteVersion: %v", err)
	}
	if cfg.Server.Port != 8088 {
		t.Fatalf("server.port = %d, want 8088", cfg.Server.Port)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatalf("mode = %v, want %v preserved", info.Mode().Perm(), originalMode)
	}
}

// FileVersion 用 YAML 解析（引号键认得出），WriteVersion 若只认顶格裸键，合法配置就会被追加
// 出第二个 version 键、整个文件随后 Load 失败。这条测试钉死两者口径一致，以及"改不了就拒绝写"。
func TestWriteVersionRewritesQuotedKeysAndRefusesWhatItCannot(t *testing.T) {
	dir := t.TempDir()

	// Legal spellings of the same top-level key: replaced in place, exactly one key left.
	for _, original := range []string{
		"\"version\": \"v1.0.0\"\nserver:\n  port: 8088\n",
		"'version': v1.0.0\nserver:\n  port: 8088\n",
		"\"version\":   v1.0.0\nserver:\n  port: 8088\n",
	} {
		path := filepath.Join(dir, "quoted.yaml")
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if v, err := FileVersion(path); err != nil || v != "v1.0.0" {
			t.Fatalf("FileVersion(%q) = %q, %v", original, v, err)
		}
		changed, err := WriteVersion(path, "v1.7.22")
		if err != nil || !changed {
			t.Fatalf("WriteVersion(%q): changed=%v err=%v", original, changed, err)
		}
		got := readTestFile(t, path)
		if strings.Count(got, "version") != 1 {
			t.Fatalf("writing %q left %d version keys:\n%q", original, strings.Count(got, "version"), got)
		}
		if v, err := FileVersion(path); err != nil || v != "v1.7.22" {
			t.Fatalf("after writing %q: FileVersion = %q, %v (file %q)", original, v, err, got)
		}
		if !strings.Contains(got, "port: 8088") {
			t.Fatalf("the rest of the file must stay: %q", got)
		}
	}

	// Forms a line-based replacement cannot rewrite: refused with the file untouched. A
	// stale version number is a display artifact; a config that no longer loads is an
	// installation that no longer starts.
	for _, original := range []string{
		"{\"version\": \"v1.0.0\", \"server\": {\"port\": 8088}}\n",
		"version: >-\n  v1.0.0\nserver:\n  port: 8088\n",
		"\"version\": \"v9.9.9\"\n\"version\": \"v1.0.0\"\n",
	} {
		path := filepath.Join(dir, "unwritable.yaml")
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		changed, err := WriteVersion(path, "v1.7.22")
		if err == nil {
			t.Fatalf("WriteVersion(%q) must refuse instead of writing: changed=%v file=%q", original, changed, readTestFile(t, path))
		}
		if got := readTestFile(t, path); got != original {
			t.Fatalf("a refused write must not touch the file:\nbefore %q\nafter  %q", original, got)
		}
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteVersionAfterCommentedDocumentStart(t *testing.T) {
	for _, prefix := range []string{"# local settings\n---\n", "\n# local settings\n--- # config\n", "%YAML 1.1\n---\n"} {
		t.Run(prefix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := prefix + "server:\n  port: 8088\n"
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if changed, err := WriteVersion(path, "v1.2.3"); err != nil || !changed {
				t.Fatalf("write: %v %v", changed, err)
			}
			cfg, err := Load(path)
			if err != nil || cfg.Server.Port != 8088 {
				t.Fatalf("settings lost after version write: cfg=%+v err=%v", cfg, err)
			}
			if got := readTestFile(t, path); got != prefix+"version: \"v1.2.3\"\nserver:\n  port: 8088\n" {
				t.Fatalf("unexpected rewrite: %q", got)
			}
		})
	}
}

func TestWriteVersionRefusesToHideAnInlineYAMLDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "--- {server: {port: 8088}}\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if changed, err := WriteVersion(path, "v1.2.3"); err == nil || changed {
		t.Fatalf("must refuse document loss: changed=%v err=%v", changed, err)
	}
	if got := readTestFile(t, path); got != original {
		t.Fatal("refusal modified config")
	}
}

func TestVersionWritePreservesConfigSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "actual.yaml")
	link := filepath.Join(dir, "config.yaml")
	original := "server:\n  port: 8088\n"
	if err := os.WriteFile(target, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteVersion(link, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("version write replaced config symlink")
	}
	if version, err := FileVersion(target); err != nil || version != "v1.2.3" {
		t.Fatal("version did not reach symlink target")
	}
	if _, err := RemoveVersion(link); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, target); got != original {
		t.Fatal("version removal changed original settings")
	}
}
