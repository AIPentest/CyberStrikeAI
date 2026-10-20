// Package modelmeta probes local inference servers for runtime model metadata.
// It is intentionally best-effort: callers treat an empty ContextWindow as "unknown".
package modelmeta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	BackendOllama           = "ollama"
	BackendLlamaCpp         = "llamacpp"
	BackendLMStudio         = "lmstudio"
	BackendOpenAICompatible = "openai_compatible"

	WarningOllamaV1IgnoresNumCtx = "ollama_v1_ignores_num_ctx"
	WarningOllamaRuntimeUnknown  = "ollama_runtime_context_unknown"
	WarningRuntimeUnknown        = "context_runtime_unknown"
	WarningContextUnknown        = "context_window_unknown"
	WarningBudgetExceedsWindow   = "configured_budget_exceeds_context_window"
	probeTimeout                 = 2 * time.Second
)

type ContextInfo struct {
	Backend        string   `json:"backend,omitempty"`
	ContextWindow  int      `json:"context_window,omitempty"`
	Source         string   `json:"context_source,omitempty"`
	Loaded         bool     `json:"context_loaded,omitempty"`
	RuntimeUnknown bool     `json:"context_runtime_unknown,omitempty"`
	Warnings       []string `json:"context_warnings,omitempty"`
}

// DetectContext probes the endpoint in local-backend-first order and returns the
// most authoritative context window it can find. It never returns an error: a
// failed probe is represented by an empty ContextWindow plus a warning.
func DetectContext(ctx context.Context, baseURL, model, apiKey string, client *http.Client) ContextInfo {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return ContextInfo{Warnings: []string{WarningContextUnknown}}
	}
	base := strings.TrimSuffix(parsed.String(), "/")
	model = strings.TrimSpace(model)

	for _, root := range probeRoots(parsed) {
		if info, ok := detectOllama(ctx, client, root, base, model, apiKey); ok {
			return info
		}
		if info, ok := detectLlamaCpp(ctx, client, root, model, apiKey); ok {
			return info
		}
		if info, ok := detectLMStudio(ctx, client, root, model, apiKey); ok {
			return info
		}
	}
	return detectOpenAICompatible(ctx, client, base, model, apiKey)
}

// probeRoots returns candidate server roots, most specific first: the base URL
// without its version suffix (covers reverse proxies such as /ollama/v1), then
// the scheme+host origin. Duplicates are removed.
func probeRoots(parsed *url.URL) []string {
	roots := make([]string, 0, 2)
	add := func(root string) {
		root = strings.TrimSuffix(strings.TrimSpace(root), "/")
		if root == "" {
			return
		}
		for _, existing := range roots {
			if existing == root {
				return
			}
		}
		roots = append(roots, root)
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/v1")
	prefix := parsed.Scheme + "://" + parsed.Host + path
	add(prefix)
	add(parsed.Scheme + "://" + parsed.Host)
	return roots
}

func parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "https://api.openai.com/v1"
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, err
	}
	return u, nil
}

func detectOllama(ctx context.Context, client *http.Client, root, base, model, apiKey string) (ContextInfo, bool) {
	var version struct {
		Version string `json:"version"`
	}
	if status, err := probeJSON(ctx, client, http.MethodGet, root+"/api/version", apiKey, nil, &version); err != nil || status != http.StatusOK {
		return ContextInfo{}, false
	}

	info := ContextInfo{Backend: BackendOllama}
	if strings.HasSuffix(strings.TrimRight(base, "/"), "/v1") {
		info.Warnings = append(info.Warnings, WarningOllamaV1IgnoresNumCtx)
	}

	if n, loaded := ollamaRuntimeContext(ctx, client, root, model, apiKey); n > 0 {
		info.ContextWindow = n
		info.Source = "ollama_ps"
		info.Loaded = loaded
		return info, true
	}

	show := struct {
		Parameters string                     `json:"parameters"`
		ModelInfo  map[string]json.RawMessage `json:"model_info"`
	}{}
	body := map[string]string{"model": model}
	if status, err := probeJSON(ctx, client, http.MethodPost, root+"/api/show", apiKey, body, &show); err == nil && status == http.StatusOK {
		if n := ollamaNumCtx(show.Parameters); n > 0 {
			info.ContextWindow = n
			info.Source = "ollama_show_num_ctx"
			return info, true
		}
		if n := ollamaModelContext(show.ModelInfo); n > 0 {
			info.ContextWindow = n
			info.Source = "ollama_show_model_max"
			info.RuntimeUnknown = true
			info.Warnings = append(info.Warnings, WarningOllamaRuntimeUnknown)
			return info, true
		}
	}
	info.RuntimeUnknown = true
	info.Warnings = append(info.Warnings, WarningOllamaRuntimeUnknown, WarningContextUnknown)
	return info, true
}

func ollamaRuntimeContext(ctx context.Context, client *http.Client, root, model, apiKey string) (int, bool) {
	var ps struct {
		Models []struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if status, err := probeJSON(ctx, client, http.MethodGet, root+"/api/ps", apiKey, nil, &ps); err != nil || status != http.StatusOK {
		return 0, false
	}
	best := 0
	for _, m := range ps.Models {
		if m.ContextLength <= 0 || !modelMatches(m.Name, model) && !modelMatches(m.Model, model) {
			continue
		}
		if m.ContextLength > best {
			best = m.ContextLength
		}
	}
	if best == 0 && len(ps.Models) == 1 && ps.Models[0].ContextLength > 0 && strings.TrimSpace(model) == "" {
		best = ps.Models[0].ContextLength
	}
	return best, best > 0
}

func ollamaNumCtx(parameters string) int {
	for _, line := range strings.Split(parameters, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "num_ctx") {
			continue
		}
		if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func ollamaModelContext(modelInfo map[string]json.RawMessage) int {
	arch := ""
	if raw, ok := modelInfo["general.architecture"]; ok {
		_ = json.Unmarshal(raw, &arch)
	}
	if arch != "" {
		if raw, ok := modelInfo[arch+".context_length"]; ok {
			if n := rawInt(raw); n > 0 {
				return n
			}
		}
	}
	best := 0
	for key, raw := range modelInfo {
		if !strings.HasSuffix(key, ".context_length") {
			continue
		}
		if n := rawInt(raw); n > best {
			best = n
		}
	}
	return best
}

func detectLlamaCpp(ctx context.Context, client *http.Client, root, model, apiKey string) (ContextInfo, bool) {
	var props struct {
		NCtx                      int `json:"n_ctx"`
		DefaultGenerationSettings struct {
			NCtx   int `json:"n_ctx"`
			Params struct {
				NCtx int `json:"n_ctx"`
			} `json:"params"`
		} `json:"default_generation_settings"`
		TotalSlots int `json:"total_slots"`
	}
	status, err := probeJSON(ctx, client, http.MethodGet, root+"/props", apiKey, nil, &props)
	if err != nil || status != http.StatusOK {
		return ContextInfo{}, false
	}
	n := props.DefaultGenerationSettings.Params.NCtx
	if n <= 0 {
		n = props.DefaultGenerationSettings.NCtx
	}
	if n <= 0 {
		n = props.NCtx
	}
	info := ContextInfo{Backend: BackendLlamaCpp, Loaded: true}
	if n > 0 {
		info.ContextWindow = n
		info.Source = "llamacpp_props"
	} else {
		info.Warnings = append(info.Warnings, WarningContextUnknown)
	}
	return info, true
}

type lmStudioModel struct {
	ID                  string             `json:"id"`
	Type                string             `json:"type"`
	State               string             `json:"state"`
	MaxContextLength    int                `json:"max_context_length"`
	LoadedContextLength int                `json:"loaded_context_length"`
	LoadedInstances     []lmStudioInstance `json:"loaded_instances"`
}

type lmStudioInstance struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"`
	Config        struct {
		ContextLength int `json:"context_length"`
	} `json:"config"`
}

func detectLMStudio(ctx context.Context, client *http.Client, root, model, apiKey string) (ContextInfo, bool) {
	var list struct {
		Data   []lmStudioModel `json:"data"`
		Models []lmStudioModel `json:"models"`
	}
	paths := []string{"/api/v0/models", "/api/v1/models"}
	for _, path := range paths {
		status, err := probeJSON(ctx, client, http.MethodGet, root+path, apiKey, nil, &list)
		if err != nil || status != http.StatusOK {
			continue
		}
		models := list.Data
		if len(models) == 0 {
			models = list.Models
		}
		chosen := pickLMModel(models, model)
		if chosen == nil {
			return ContextInfo{Backend: BackendLMStudio, Warnings: []string{WarningContextUnknown}}, true
		}
		info := ContextInfo{Backend: BackendLMStudio}
		if chosen.LoadedContextLength > 0 {
			info.ContextWindow = chosen.LoadedContextLength
			info.Source = "lmstudio_loaded_context"
			info.Loaded = true
			return info, true
		}
		for _, inst := range chosen.LoadedInstances {
			if inst.ContextLength > 0 {
				info.ContextWindow = inst.ContextLength
				info.Source = "lmstudio_loaded_instance"
				info.Loaded = true
				return info, true
			}
			if inst.Config.ContextLength > 0 {
				info.ContextWindow = inst.Config.ContextLength
				info.Source = "lmstudio_loaded_instance_config"
				info.Loaded = true
				return info, true
			}
		}
		if chosen.MaxContextLength > 0 {
			info.ContextWindow = chosen.MaxContextLength
			info.Source = "lmstudio_model_max"
			info.RuntimeUnknown = true
			info.Warnings = append(info.Warnings, WarningRuntimeUnknown)
			return info, true
		}
		info.Warnings = append(info.Warnings, WarningContextUnknown)
		return info, true
	}
	return ContextInfo{}, false
}

func pickLMModel(models []lmStudioModel, wanted string) *lmStudioModel {
	for i := range models {
		if modelMatches(models[i].ID, wanted) {
			return &models[i]
		}
	}
	if len(models) == 1 {
		return &models[0]
	}
	return nil
}

func detectOpenAICompatible(ctx context.Context, client *http.Client, base, model, apiKey string) ContextInfo {
	var list struct {
		Data []struct {
			ID               string          `json:"id"`
			MaxModelLen      int             `json:"max_model_len"`
			ContextLength    json.RawMessage `json:"context_length"`
			MaxContextLength int             `json:"max_context_length"`
		} `json:"data"`
	}
	status, err := probeJSON(ctx, client, http.MethodGet, strings.TrimRight(base, "/")+"/models", apiKey, nil, &list)
	if err != nil || status != http.StatusOK {
		return ContextInfo{Backend: BackendOpenAICompatible, Warnings: []string{WarningContextUnknown}}
	}
	info := ContextInfo{Backend: BackendOpenAICompatible}
	chosen := -1
	for i := range list.Data {
		if modelMatches(list.Data[i].ID, model) {
			chosen = i
			break
		}
	}
	if chosen < 0 && len(list.Data) == 1 {
		chosen = 0
	}
	if chosen >= 0 {
		m := list.Data[chosen]
		switch {
		case m.MaxModelLen > 0:
			info.ContextWindow = m.MaxModelLen
			info.Source = "openai_models_max_model_len"
		case rawInt(m.ContextLength) > 0:
			info.ContextWindow = rawInt(m.ContextLength)
			info.Source = "openai_models_context_length"
		case m.MaxContextLength > 0:
			info.ContextWindow = m.MaxContextLength
			info.Source = "openai_models_max_context_length"
		}
		if info.ContextWindow > 0 {
			info.Loaded = true
			return info
		}
	}
	info.Warnings = append(info.Warnings, WarningContextUnknown)
	return info
}

func probeJSON(ctx context.Context, client *http.Client, method, target, apiKey string, body interface{}, out interface{}) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(reqCtx, method, target, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil
	}
	if out == nil || len(raw) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func rawInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(s))
	}
	return n
}

func modelMatches(candidate, wanted string) bool {
	c := strings.ToLower(strings.TrimSpace(candidate))
	w := strings.ToLower(strings.TrimSpace(wanted))
	if c == "" || w == "" {
		return false
	}
	if c == w || strings.TrimSuffix(c, ":latest") == strings.TrimSuffix(w, ":latest") {
		return true
	}
	return strings.HasSuffix(c, "/"+w) || strings.HasSuffix(w, "/"+c)
}
