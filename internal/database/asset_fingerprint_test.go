package database

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestDeleteAssetFingerprints(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "fp-delete.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("创建测试库失败: %v", err)
	}
	defer db.Close()

	asset := &Asset{Host: "a.example.com", Domain: "example.com", Port: 443, Protocol: "https"}
	if _, err := db.UpsertAssets([]*Asset{asset}, ""); err != nil {
		t.Fatal(err)
	}
	items := []AssetFingerprintInput{
		{Service: "http", Technology: "nginx", Version: "1.22.0"},
		{Service: "http", Technology: "php", Version: "8.1"},
	}
	if err := db.UpsertAssetFingerprints(asset.ID, items); err != nil {
		t.Fatal(err)
	}

	// 精确删除 nginx
	n, err := db.DeleteAssetFingerprints("http", "nginx", "1.22.0")
	if err != nil || n != 1 {
		t.Fatalf("删除条数 = %d, want 1: %v", n, err)
	}
	count, _ := db.CountAssetFingerprints(asset.ID)
	if count != 1 {
		t.Fatalf("剩余指纹 = %d, want 1", count)
	}

	// 不指定任何条件必须报错，避免误删全表
	if _, err := db.DeleteAssetFingerprints("", "", ""); err == nil {
		t.Fatal("空条件应被拒绝")
	}

	// 按服务删除剩余
	if n, err := db.DeleteAssetFingerprints("http", "", ""); err != nil || n != 1 {
		t.Fatalf("按服务删除 = %d, want 1: %v", n, err)
	}
}
