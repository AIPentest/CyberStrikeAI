package database

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// copyRepoDBToTemp 把仓库 data/ 下的真实数据库复制到 t.TempDir()，返回副本路径。
//
// 背景：audit_time_test / project_time_test 需要一份「有真实数据」的库来验证时间字段
// 的多种存储格式。它们原先直接 open 仓库里的 data/conversations.db，而该文件是运行时
// 产物、在部署机上通常属于 root 且对当前用户只读；NewDB 会执行 schema 补齐
// （建表 / 加字段），一旦缺表就会写库并报 "attempt to write a readonly database"。
//
// 复制一份到临时目录既能保留真实数据，又不依赖生产库的写权限，也不会污染它。
func copyRepoDBToTemp(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Skip(err)
	}
	source := filepath.Join(root, "..", "..", "data", "conversations.db")
	if _, err := os.Stat(source); err != nil {
		t.Skip("conversations.db not found")
	}
	target := filepath.Join(t.TempDir(), "conversations.db")
	if err := copyFileForTest(source, target); err != nil {
		t.Skipf("复制 conversations.db 失败: %v", err)
	}
	// WAL/SHM 里可能还有未合并的数据，存在就一并复制。
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(source + suffix); err == nil {
			_ = copyFileForTest(source+suffix, target+suffix)
		}
	}
	return target
}

func copyFileForTest(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
