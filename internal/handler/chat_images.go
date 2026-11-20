package handler

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/multiagent"

	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

const (
	maxChatImageBytes = 8 << 20
	maxChatImageTotal = 12 << 20
)

func (h *AgentHandler) authorizeChatAttachmentPaths(c *gin.Context, attachments []ChatAttachment) error {
	uploads := NewChatUploadsHandler(h.logger, h.db)
	root, err := filepath.Abs(chatUploadsDirName)
	if err != nil {
		return err
	}
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.ServerPath) == "" {
			continue
		}
		path, err := validateChatAttachmentServerPath(attachment.ServerPath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || !uploads.pathAllowed(c, relative) {
			return fmt.Errorf("无权访问附件 %s", attachment.FileName)
		}
	}
	return nil
}

func loadChatImages(paths []string) ([]agentruntime.Image, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	rootPath, err := filepath.Abs(chatUploadsDirName)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var images []agentruntime.Image
	total := 0
	for _, path := range paths {
		relative, err := filepath.Rel(rootPath, path)
		if err != nil {
			return nil, err
		}
		img, err := readChatImage(root, relative, maxChatImageTotal-total)
		if err != nil {
			return nil, fmt.Errorf("读取附件 %s: %w", filepath.Base(path), err)
		}
		if img.Data != nil {
			images = append(images, img)
			total += len(img.Data)
		}
	}
	return images, nil
}

func readChatImage(root *os.Root, name string, remaining int) (agentruntime.Image, error) {
	var fileImage agentruntime.Image
	if !filepath.IsLocal(name) {
		return fileImage, fmt.Errorf("附件路径无效")
	}
	for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil {
			return fileImage, err
		}
		if !info.IsDir() {
			return fileImage, fmt.Errorf("附件目录无效")
		}
	}
	info, err := root.Lstat(name)
	if err != nil {
		return fileImage, err
	}
	if !info.Mode().IsRegular() {
		return fileImage, fmt.Errorf("附件必须是普通文件")
	}
	f, err := root.Open(name)
	if err != nil {
		return fileImage, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return fileImage, err
	}
	if !os.SameFile(info, opened) {
		return fileImage, fmt.Errorf("附件已变更，请重新上传")
	}
	var header [512]byte
	n, err := io.ReadFull(f, header[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fileImage, err
	}
	mime := http.DetectContentType(header[:n])
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return fileImage, nil
	}
	limit := min(maxChatImageBytes, remaining)
	if opened.Size() > int64(limit) || n > limit {
		return fileImage, fmt.Errorf("图片超过单张 8 MiB 或单次合计 12 MiB 的限制")
	}
	data, err := io.ReadAll(io.LimitReader(io.MultiReader(bytes.NewReader(header[:n]), f), int64(limit)+1))
	if err != nil {
		return fileImage, err
	}
	if len(data) > limit {
		return fileImage, fmt.Errorf("图片超过单张 8 MiB 或单次合计 12 MiB 的限制")
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || dimensions.Width <= 0 || dimensions.Height <= 0 {
		return fileImage, fmt.Errorf("图片格式无效")
	}
	if dimensions.Width > 8192 || dimensions.Height > 8192 || dimensions.Width*dimensions.Height > 32<<20 {
		return fileImage, fmt.Errorf("图片分辨率过大")
	}
	return agentruntime.Image{MIMEType: mime, Data: data}, nil
}

func (p *multiAgentPrepared) withChatImages(ctx context.Context, message string) context.Context {
	if message == p.FinalMessage {
		return multiagent.WithChatImages(ctx, p.Images)
	}
	return ctx
}
