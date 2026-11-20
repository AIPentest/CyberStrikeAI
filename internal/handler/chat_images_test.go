package handler

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestReadChatImageChecksContentAndRejectsLinks(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "upload.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 24, 24))); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.png"), []byte("plain text"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	img, err := readChatImage(root, "upload.txt", maxChatImageTotal)
	if err != nil || img.MIMEType != "image/png" || len(img.Data) == 0 {
		t.Fatalf("image mime/bytes/error = %q/%d/%v", img.MIMEType, len(img.Data), err)
	}
	ignored, err := readChatImage(root, "notes.png", maxChatImageTotal)
	if err != nil || len(ignored.Data) != 0 {
		t.Fatalf("plain text treated as image: %v, %d bytes", err, len(ignored.Data))
	}
	if _, err := readChatImage(root, "upload.txt", 1); err == nil {
		t.Fatal("image exceeded the remaining size budget")
	}
	if err := os.Symlink("upload.txt", filepath.Join(dir, "link.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readChatImage(root, "link.png", maxChatImageTotal); err == nil {
		t.Fatal("symlink was accepted as an image attachment")
	}
}
