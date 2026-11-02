package screenshot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestBuildURL(t *testing.T) {
	cases := []struct {
		protocol string
		host     string
		port     int
		want     string
	}{
		{"https", "example.com", 443, "https://example.com"},
		{"https", "example.com", 8443, "https://example.com:8443"},
		{"http", "example.com", 80, "http://example.com"},
		{"http", "example.com", 8080, "http://example.com:8080"},
		{"https", "https://example.com/x", 0, "https://example.com/x"},
		{"ssh", "example.com", 22, ""},
		{"http", "", 80, ""},
	}
	for _, tc := range cases {
		if got := BuildURL(tc.protocol, tc.host, tc.port); got != tc.want {
			t.Errorf("BuildURL(%q,%q,%d) = %q, want %q", tc.protocol, tc.host, tc.port, got, tc.want)
		}
	}
}

// fakeRenderService 模拟外部 Node 截图服务。
func fakeRenderService(t *testing.T, calls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		atomic.AddInt32(calls, 1)
		var req struct {
			URL  string `json:"url"`
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		// 真实服务会把 PNG 写到 req.Path，这里写一个最小合法 PNG。
		if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
			t.Errorf("mkdir: %v", err)
		}
		if err := os.WriteFile(req.Path, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
			t.Errorf("write png: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "path": req.Path, "status_code": 200,
			"title": "Example Domain", "width": 1280, "height": 800, "error": "",
		})
	}))
}

// seedAsset 插一条资产（asset_screenshots 有外键，必须先有资产）。
func seedAsset(t *testing.T, db *database.DB, host string, port int, protocol string) string {
	t.Helper()
	asset := &database.Asset{Host: host, Port: port, Protocol: protocol, Source: "test"}
	if _, err := db.UpsertAssets([]*database.Asset{asset}, ""); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	return asset.ID
}

func newTestService(t *testing.T, serviceURL, dir string, minInterval time.Duration) (*Service, *database.DB) {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "shot.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	svc := New(Options{
		ServiceURL:     serviceURL,
		Dir:            dir,
		Timeout:        5 * time.Second,
		Concurrency:    1,
		ViewportWidth:  1280,
		ViewportHeight: 800,
		MinInterval:    minInterval,
	}, db, zap.NewNop())
	if svc == nil {
		t.Fatal("service 未创建")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.Start(ctx)
	t.Cleanup(svc.Stop)
	return svc, db
}

func TestCaptureWritesRecordAndFile(t *testing.T) {
	var calls int32
	render := fakeRenderService(t, &calls)
	defer render.Close()

	dir := t.TempDir()
	svc, db := newTestService(t, render.URL, dir, 0)
	assetID := seedAsset(t, db, "example.com", 443, "https")

	if !svc.Enqueue(assetID, "https://example.com", true) {
		t.Fatal("Enqueue 应返回 true")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		meta, err := db.GetAssetScreenshot(assetID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if meta != nil && meta.Status == database.AssetScreenshotStatusDone {
			if meta.StatusCode != 200 || meta.Title != "Example Domain" {
				t.Fatalf("截图结果异常: %+v", meta)
			}
			if _, err := os.Stat(meta.FilePath); err != nil {
				t.Fatalf("截图文件不存在: %v", err)
			}
			if atomic.LoadInt32(&calls) != 1 {
				t.Fatalf("渲染调用次数 = %d, want 1", calls)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待截图超时，最后状态: %+v", meta)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestEnqueueRespectsMinInterval(t *testing.T) {
	var calls int32
	render := fakeRenderService(t, &calls)
	defer render.Close()

	dir := t.TempDir()
	svc, db := newTestService(t, render.URL, dir, time.Hour)
	assetID := seedAsset(t, db, "example.com", 443, "https")

	if !svc.Enqueue(assetID, "https://example.com", false) {
		t.Fatal("首次应入队")
	}
	// 等第一次完成
	deadline := time.Now().Add(10 * time.Second)
	for {
		meta, _ := db.GetAssetScreenshot(assetID)
		if meta != nil && meta.Status == database.AssetScreenshotStatusDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("首次截图超时")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 最小间隔内再次自动入队应被跳过
	if svc.Enqueue(assetID, "https://example.com", false) {
		t.Fatal("最小间隔内不应重复入队")
	}
	// 强制入队仍然生效
	if !svc.Enqueue(assetID, "https://example.com", true) {
		t.Fatal("force=true 应入队")
	}
}

func TestHealthy(t *testing.T) {
	var calls int32
	render := fakeRenderService(t, &calls)
	defer render.Close()
	svc, _ := newTestService(t, render.URL, t.TempDir(), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !svc.Healthy(ctx) {
		t.Fatal("应当健康")
	}

	down := New(Options{ServiceURL: "http://127.0.0.1:1", Dir: t.TempDir(), Timeout: time.Second}, nil, zap.NewNop())
	if down != nil {
		t.Fatal("db 为 nil 时不应创建服务")
	}
}
