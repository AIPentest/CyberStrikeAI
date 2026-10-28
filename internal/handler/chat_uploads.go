package handler

import (
	"archive/zip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	chatUploadsRootDirName       = "chat_uploads"
	reductionRootDirName         = "tmp/reduction"
	workspaceRootDirName         = "tmp/workspace"
	artifactsRootDirName         = "data/conversation_artifacts"
	reductionVirtualPrefix       = "__reduction__/"
	workspaceVirtualPrefix       = "__workspace__/"
	artifactVirtualPrefix        = "__conversation_artifact__/"
	chatUploadSourceUpload       = "upload"
	chatUploadSourceReduction    = "reduction"
	chatUploadSourceWorkspace    = "workspace"
	chatUploadSourceConversation = "conversation_artifact"
	maxChatUploadEditBytes       = 2 * 1024 * 1024 // 文本编辑上限
)

// ChatUploadsHandler 对话中上传附件（chat_uploads 目录）的管理 API
type ChatUploadsHandler struct {
	logger *zap.Logger
	audit  *audit.Service
	db     *database.DB
}

// SetAudit wires platform audit logging.
func (h *ChatUploadsHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewChatUploadsHandler 创建处理器
func NewChatUploadsHandler(logger *zap.Logger, databases ...*database.DB) *ChatUploadsHandler {
	h := &ChatUploadsHandler{logger: logger}
	if len(databases) > 0 {
		h.db = databases[0]
	}
	return h
}

func (h *ChatUploadsHandler) pathAllowed(c *gin.Context, relativePath string) bool {
	session, ok := security.CurrentSession(c)
	if !ok || h.db == nil {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(relativePath))))
	rel = strings.Trim(rel, "/")
	if conversationID, ownerUserID, found := h.db.GetChatUploadArtifact(rel); found {
		return strings.TrimSpace(ownerUserID) == session.UserID || h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", conversationID)
	}
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) < 2 || parts[1] == "" || parts[1] == "_manual" {
		return false
	}
	return h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", parts[1])
}

func (h *ChatUploadsHandler) reductionPathAllowed(c *gin.Context, scope, id string) bool {
	session, ok := security.CurrentSession(c)
	if !ok || h.db == nil {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	id = strings.TrimSpace(id)
	switch scope {
	case "conversations":
		if id == "" || id == "default" {
			return false
		}
		return h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", id)
	case "projects":
		if id == "" {
			return false
		}
		return h.db.UserCanAccessResource(session.UserID, session.Scope, "project", id)
	default:
		return false
	}
}

func (h *ChatUploadsHandler) reductionVirtualPathAllowed(c *gin.Context, relativePath string) bool {
	rel := strings.TrimPrefix(strings.TrimSpace(relativePath), reductionVirtualPrefix)
	rel = strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))), "/")
	if rel == "projects" || rel == "conversations" {
		_, ok := security.CurrentSession(c)
		return ok
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return false
	}
	return h.reductionPathAllowed(c, parts[0], parts[1])
}

func (h *ChatUploadsHandler) workspacePathAllowed(c *gin.Context, scope, id string) bool {
	return h.reductionPathAllowed(c, scope, id)
}

func (h *ChatUploadsHandler) workspaceVirtualPathAllowed(c *gin.Context, relativePath string) bool {
	rel := strings.TrimPrefix(strings.TrimSpace(relativePath), workspaceVirtualPrefix)
	rel = strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))), "/")
	if rel == "projects" || rel == "conversations" {
		_, ok := security.CurrentSession(c)
		return ok
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return false
	}
	return h.workspacePathAllowed(c, parts[0], parts[1])
}

func (h *ChatUploadsHandler) conversationArtifactPathAllowed(c *gin.Context, conversationID string) bool {
	session, ok := security.CurrentSession(c)
	if !ok || h.db == nil {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || conversationID == "default" {
		return false
	}
	return h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", conversationID)
}

func (h *ChatUploadsHandler) conversationArtifactVirtualPathAllowed(c *gin.Context, relativePath string) bool {
	rel := strings.TrimPrefix(strings.TrimSpace(relativePath), artifactVirtualPrefix)
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 1 {
		return false
	}
	return h.conversationArtifactPathAllowed(c, parts[0])
}

func (h *ChatUploadsHandler) absRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(cwd, chatUploadsRootDirName))
}

func (h *ChatUploadsHandler) absReductionRoot() (string, error) {
	if h.db != nil {
		if base := strings.TrimSpace(h.db.EinoReductionBaseDir()); base != "" {
			if filepath.IsAbs(base) {
				return filepath.Abs(base)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			return filepath.Abs(filepath.Join(cwd, base))
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(cwd, reductionRootDirName))
}

func (h *ChatUploadsHandler) absWorkspaceRoot() (string, error) {
	if h.db != nil {
		if base := strings.TrimSpace(h.db.EinoWorkspaceBaseDir()); base != "" {
			if filepath.IsAbs(base) {
				return filepath.Abs(base)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			return filepath.Abs(filepath.Join(cwd, base))
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(cwd, workspaceRootDirName))
}

func (h *ChatUploadsHandler) absConversationArtifactsRoot() (string, error) {
	if h.db != nil {
		if base := strings.TrimSpace(h.db.ConversationArtifactsBaseDir()); base != "" {
			if filepath.IsAbs(base) {
				return filepath.Abs(base)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			return filepath.Abs(filepath.Join(cwd, base))
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(cwd, artifactsRootDirName))
}

// resolveUnderChatUploads 校验 relativePath（使用 / 分隔）对应文件必须在 chat_uploads 根下
func (h *ChatUploadsHandler) resolveUnderChatUploads(relativePath string) (abs string, err error) {
	root, err := h.absRoot()
	if err != nil {
		return "", err
	}
	rel := strings.TrimSpace(relativePath)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(root, rel)
	full, err = filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootAbs, _ := filepath.Abs(root)
	if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes chat_uploads root")
	}
	return full, nil
}

// ChatUploadFileItem 列表项
type ChatUploadFileItem struct {
	RelativePath      string `json:"relativePath"`
	AbsolutePath      string `json:"absolutePath"` // 服务器上的绝对路径，便于在对话中引用（与附件落盘路径一致）
	Name              string `json:"name"`
	Source            string `json:"source,omitempty"`
	Size              int64  `json:"size"`
	ModifiedUnix      int64  `json:"modifiedUnix"`
	Date              string `json:"date"`
	ConversationID    string `json:"conversationId"`
	ConversationTitle string `json:"conversationTitle,omitempty"`
	ProjectID         string `json:"projectId,omitempty"`
	ProjectName       string `json:"projectName,omitempty"`
	// SubPath 为日期、会话目录之下的子路径（不含文件名），如 date/conv/a/b/file 则为 "a/b"；无嵌套则为 ""。
	SubPath string `json:"subPath"`
}

func (h *ChatUploadsHandler) conversationProjectID(conversationID string, cache map[string]string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || conversationID == "_manual" || conversationID == "_new" || h.db == nil {
		return ""
	}
	if v, ok := cache[conversationID]; ok {
		return v
	}
	projectID, err := h.db.GetConversationProjectID(conversationID)
	if err != nil {
		projectID = ""
	}
	cache[conversationID] = projectID
	return projectID
}

func (h *ChatUploadsHandler) conversationTitle(conversationID string, cache map[string]string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || conversationID == "_manual" || conversationID == "_new" || h.db == nil {
		return ""
	}
	if v, ok := cache[conversationID]; ok {
		return v
	}
	title, err := h.db.GetConversationTitle(conversationID)
	if err != nil {
		title = ""
	}
	cache[conversationID] = title
	return title
}

func (h *ChatUploadsHandler) projectName(projectID string, cache map[string]string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || h.db == nil {
		return ""
	}
	if v, ok := cache[projectID]; ok {
		return v
	}
	name, err := h.db.GetProjectName(projectID)
	if err != nil {
		name = ""
	}
	cache[projectID] = name
	return name
}

func (h *ChatUploadsHandler) collectFiles(c *gin.Context, conversationFilter, projectFilter string) ([]ChatUploadFileItem, []string, error) {
	root, err := h.absRoot()
	if err != nil {
		return nil, nil, err
	}
	// 保证根目录存在，否则「按文件夹」浏览时无法 mkdir，且首次列表为空时界面无路径工具栏
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, nil, err
	}
	var files []ChatUploadFileItem
	var folders []string
	projectCache := make(map[string]string)
	projectNameCache := make(map[string]string)
	conversationTitleCache := make(map[string]string)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			folders = append(folders, relSlash)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		parts := strings.Split(relSlash, "/")
		var dateStr, convID string
		if len(parts) >= 2 {
			dateStr = parts[0]
		}
		if len(parts) >= 3 {
			convID = parts[1]
		}
		projectID := h.conversationProjectID(convID, projectCache)
		var subPath string
		if len(parts) >= 4 {
			subPath = strings.Join(parts[2:len(parts)-1], "/")
		}
		if conversationFilter != "" && convID != conversationFilter {
			return nil
		}
		if projectFilter != "" && projectID != projectFilter {
			return nil
		}
		absPath, _ := filepath.Abs(path)
		files = append(files, ChatUploadFileItem{
			RelativePath:      relSlash,
			AbsolutePath:      absPath,
			Name:              d.Name(),
			Source:            chatUploadSourceUpload,
			Size:              info.Size(),
			ModifiedUnix:      info.ModTime().Unix(),
			Date:              dateStr,
			ConversationID:    convID,
			ConversationTitle: h.conversationTitle(convID, conversationTitleCache),
			ProjectID:         projectID,
			ProjectName:       h.projectName(projectID, projectNameCache),
			SubPath:           subPath,
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if conversationFilter != "" || projectFilter != "" {
		filteredFolders := make([]string, 0, len(folders))
		for _, rel := range folders {
			parts := strings.Split(rel, "/")
			if len(parts) >= 2 && (conversationFilter == "" || parts[1] == conversationFilter) && (projectFilter == "" || h.conversationProjectID(parts[1], projectCache) == projectFilter) {
				filteredFolders = append(filteredFolders, rel)
				continue
			}
			if len(parts) == 1 {
				prefix := rel + "/"
				for _, f := range files {
					if strings.HasPrefix(f.RelativePath, prefix) {
						filteredFolders = append(filteredFolders, rel)
						break
					}
				}
			}
		}
		folders = filteredFolders
	}
	files = filterSlice(files, func(file ChatUploadFileItem) bool {
		return h.pathAllowed(c, file.RelativePath)
	})
	folders = filterSlice(folders, func(folder string) bool {
		if h.pathAllowed(c, folder) {
			return true
		}
		prefix := strings.TrimSuffix(folder, "/") + "/"
		for _, file := range files {
			if strings.HasPrefix(file.RelativePath, prefix) {
				return true
			}
		}
		return false
	})
	sort.Strings(folders)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModifiedUnix > files[j].ModifiedUnix
	})
	reductionFiles, err := h.collectReductionFiles(c, conversationFilter, projectFilter)
	if err != nil {
		h.logger.Warn("failed to list reduction artifacts", zap.Error(err))
	} else if len(reductionFiles) > 0 {
		files = append(files, reductionFiles...)
	}
	workspaceFiles, err := h.collectWorkspaceFiles(c, conversationFilter, projectFilter)
	if err != nil {
		h.logger.Warn("failed to list workspace artifacts", zap.Error(err))
	} else if len(workspaceFiles) > 0 {
		files = append(files, workspaceFiles...)
	}
	artifactFiles, err := h.collectConversationArtifactFiles(c, conversationFilter, projectFilter)
	if err != nil {
		h.logger.Warn("failed to list conversation_artifacts artifacts", zap.Error(err))
	} else if len(artifactFiles) > 0 {
		files = append(files, artifactFiles...)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModifiedUnix > files[j].ModifiedUnix
	})
	return files, folders, nil
}

func (h *ChatUploadsHandler) collectReductionFiles(c *gin.Context, conversationFilter, projectFilter string) ([]ChatUploadFileItem, error) {
	root, err := h.absReductionRoot()
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, nil
	}
	projectCache := make(map[string]string)
	projectNameCache := make(map[string]string)
	conversationTitleCache := make(map[string]string)
	files := make([]ChatUploadFileItem, 0)
	for _, scope := range []string{"conversations", "projects"} {
		scopeRoot := filepath.Join(root, scope)
		_ = filepath.WalkDir(scopeRoot, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			parts := strings.Split(relSlash, "/")
			if len(parts) < 3 {
				return nil
			}
			ownerID := parts[1]
			if !h.reductionPathAllowed(c, scope, ownerID) {
				return nil
			}
			var convID, projectID string
			if scope == "conversations" {
				convID = ownerID
				projectID = h.conversationProjectID(convID, projectCache)
			} else {
				projectID = ownerID
			}
			if conversationFilter != "" && convID != conversationFilter {
				if scope != "projects" || h.conversationProjectID(conversationFilter, projectCache) != projectID {
					return nil
				}
				convID = conversationFilter
			}
			if projectFilter != "" && projectID != projectFilter {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			name := d.Name()
			if filepath.Ext(name) == "" {
				name += ".txt"
			}
			abs, _ := filepath.Abs(path)
			files = append(files, ChatUploadFileItem{
				RelativePath:      reductionVirtualPrefix + relSlash,
				AbsolutePath:      abs,
				Name:              name,
				Source:            chatUploadSourceReduction,
				Size:              info.Size(),
				ModifiedUnix:      info.ModTime().Unix(),
				Date:              info.ModTime().Format("2006-01-02"),
				ConversationID:    convID,
				ConversationTitle: h.conversationTitle(convID, conversationTitleCache),
				ProjectID:         projectID,
				ProjectName:       h.projectName(projectID, projectNameCache),
				SubPath:           strings.Join(parts[2:len(parts)-1], "/"),
			})
			return nil
		})
	}
	return files, nil
}

func (h *ChatUploadsHandler) collectWorkspaceFiles(c *gin.Context, conversationFilter, projectFilter string) ([]ChatUploadFileItem, error) {
	root, err := h.absWorkspaceRoot()
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, nil
	}
	projectCache := make(map[string]string)
	projectNameCache := make(map[string]string)
	conversationTitleCache := make(map[string]string)
	files := make([]ChatUploadFileItem, 0)
	for _, scope := range []string{"conversations", "projects"} {
		scopeRoot := filepath.Join(root, scope)
		_ = filepath.WalkDir(scopeRoot, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			parts := strings.Split(relSlash, "/")
			if len(parts) < 3 {
				return nil
			}
			ownerID := parts[1]
			if !h.workspacePathAllowed(c, scope, ownerID) {
				return nil
			}
			var convID, projectID string
			if scope == "conversations" {
				convID = ownerID
				projectID = h.conversationProjectID(convID, projectCache)
			} else {
				projectID = ownerID
			}
			if conversationFilter != "" && convID != conversationFilter {
				if scope != "projects" || h.conversationProjectID(conversationFilter, projectCache) != projectID {
					return nil
				}
				convID = conversationFilter
			}
			if projectFilter != "" && projectID != projectFilter {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			abs, _ := filepath.Abs(path)
			files = append(files, ChatUploadFileItem{
				RelativePath:      workspaceVirtualPrefix + relSlash,
				AbsolutePath:      abs,
				Name:              d.Name(),
				Source:            chatUploadSourceWorkspace,
				Size:              info.Size(),
				ModifiedUnix:      info.ModTime().Unix(),
				Date:              info.ModTime().Format("2006-01-02"),
				ConversationID:    convID,
				ConversationTitle: h.conversationTitle(convID, conversationTitleCache),
				ProjectID:         projectID,
				ProjectName:       h.projectName(projectID, projectNameCache),
				SubPath:           strings.Join(parts[2:len(parts)-1], "/"),
			})
			return nil
		})
	}
	return files, nil
}

func (h *ChatUploadsHandler) collectConversationArtifactFiles(c *gin.Context, conversationFilter, projectFilter string) ([]ChatUploadFileItem, error) {
	root, err := h.absConversationArtifactsRoot()
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, nil
	}
	projectCache := make(map[string]string)
	projectNameCache := make(map[string]string)
	conversationTitleCache := make(map[string]string)
	files := make([]ChatUploadFileItem, 0)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		if len(parts) < 2 {
			return nil
		}
		convID := parts[0]
		if !h.conversationArtifactPathAllowed(c, convID) {
			return nil
		}
		projectID := h.conversationProjectID(convID, projectCache)
		if conversationFilter != "" && convID != conversationFilter {
			return nil
		}
		if projectFilter != "" && projectID != projectFilter {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		name := d.Name()
		if filepath.Ext(name) == "" {
			name += ".txt"
		}
		abs, _ := filepath.Abs(path)
		files = append(files, ChatUploadFileItem{
			RelativePath:      artifactVirtualPrefix + relSlash,
			AbsolutePath:      abs,
			Name:              name,
			Source:            chatUploadSourceConversation,
			Size:              info.Size(),
			ModifiedUnix:      info.ModTime().Unix(),
			Date:              info.ModTime().Format("2006-01-02"),
			ConversationID:    convID,
			ConversationTitle: h.conversationTitle(convID, conversationTitleCache),
			ProjectID:         projectID,
			ProjectName:       h.projectName(projectID, projectNameCache),
			SubPath:           strings.Join(parts[1:len(parts)-1], "/"),
		})
		return nil
	})
	return files, nil
}

func (h *ChatUploadsHandler) resolveReductionVirtualPath(relativePath string) (string, error) {
	rel := strings.TrimPrefix(strings.TrimSpace(relativePath), reductionVirtualPrefix)
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	root, err := h.absReductionRoot()
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	rootAbs, _ := filepath.Abs(root)
	if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes reduction root")
	}
	return full, nil
}

func (h *ChatUploadsHandler) resolveWorkspaceVirtualPath(relativePath string) (string, error) {
	rel := strings.TrimPrefix(strings.TrimSpace(relativePath), workspaceVirtualPrefix)
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	root, err := h.absWorkspaceRoot()
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	rootAbs, _ := filepath.Abs(root)
	if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return full, nil
}

func (h *ChatUploadsHandler) resolveConversationArtifactVirtualPath(relativePath string) (string, error) {
	rel := strings.TrimPrefix(strings.TrimSpace(relativePath), artifactVirtualPrefix)
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	root, err := h.absConversationArtifactsRoot()
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	rootAbs, _ := filepath.Abs(root)
	if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes conversation artifacts root")
	}
	return full, nil
}

func chatUploadItemIsInternal(item ChatUploadFileItem) bool {
	return item.Source == chatUploadSourceReduction ||
		item.Source == chatUploadSourceWorkspace ||
		item.Source == chatUploadSourceConversation ||
		strings.HasPrefix(item.RelativePath, reductionVirtualPrefix) ||
		strings.HasPrefix(item.RelativePath, workspaceVirtualPrefix) ||
		strings.HasPrefix(item.RelativePath, artifactVirtualPrefix)
}

func (h *ChatUploadsHandler) resolveListedFilePath(item ChatUploadFileItem) (string, error) {
	switch {
	case item.Source == chatUploadSourceReduction || strings.HasPrefix(item.RelativePath, reductionVirtualPrefix):
		return h.resolveReductionVirtualPath(item.RelativePath)
	case item.Source == chatUploadSourceWorkspace || strings.HasPrefix(item.RelativePath, workspaceVirtualPrefix):
		return h.resolveWorkspaceVirtualPath(item.RelativePath)
	case item.Source == chatUploadSourceConversation || strings.HasPrefix(item.RelativePath, artifactVirtualPrefix):
		return h.resolveConversationArtifactVirtualPath(item.RelativePath)
	default:
		return h.resolveUnderChatUploads(item.RelativePath)
	}
}

func chatUploadSourceMatches(item ChatUploadFileItem, sourceFilter string) bool {
	sourceFilter = strings.TrimSpace(sourceFilter)
	if sourceFilter == "" || sourceFilter == "all" {
		return true
	}
	source := strings.TrimSpace(item.Source)
	if source == "" {
		source = chatUploadSourceUpload
	}
	return source == sourceFilter
}

func chatUploadSearchMatches(item ChatUploadFileItem, search string) bool {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return true
	}
	values := []string{
		item.RelativePath,
		item.Name,
		item.Source,
		item.Date,
		item.ConversationID,
		item.ProjectID,
		item.SubPath,
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}

func filterChatUploadItems(files []ChatUploadFileItem, sourceFilter, search string) []ChatUploadFileItem {
	if strings.TrimSpace(sourceFilter) == "" && strings.TrimSpace(search) == "" {
		return files
	}
	out := make([]ChatUploadFileItem, 0, len(files))
	for _, item := range files {
		if chatUploadSourceMatches(item, sourceFilter) && chatUploadSearchMatches(item, search) {
			out = append(out, item)
		}
	}
	return out
}

func parsePositiveIntQuery(c *gin.Context, key string, def, max int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def
	}
	if strings.EqualFold(raw, "all") {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

func paginateChatUploadItems(files []ChatUploadFileItem, page, pageSize int) []ChatUploadFileItem {
	if pageSize <= 0 {
		return files
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(files) {
		return []ChatUploadFileItem{}
	}
	end := start + pageSize
	if end > len(files) {
		end = len(files)
	}
	return files[start:end]
}

// List GET /api/chat-uploads
func (h *ChatUploadsHandler) List(c *gin.Context) {
	conversationFilter := strings.TrimSpace(c.Query("conversation"))
	projectFilter := strings.TrimSpace(c.Query("project"))
	sourceFilter := strings.TrimSpace(c.Query("source"))
	search := strings.TrimSpace(c.Query("search"))
	page := parsePositiveIntQuery(c, "page", 1, 0)
	pageSize := parsePositiveIntQuery(c, "pageSize", 20, 200)
	files, folders, err := h.collectFiles(c, conversationFilter, projectFilter)
	if err != nil {
		h.logger.Warn("failed to list chat attachments", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	files = filterChatUploadItems(files, sourceFilter, search)
	total := len(files)
	paged := paginateChatUploadItems(files, page, pageSize)
	c.JSON(http.StatusOK, gin.H{
		"files":    paged,
		"folders":  folders,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// Export GET /api/chat-uploads/export?conversation=...&project=...
func (h *ChatUploadsHandler) Export(c *gin.Context) {
	conversationFilter := strings.TrimSpace(c.Query("conversation"))
	projectFilter := strings.TrimSpace(c.Query("project"))
	sourceFilter := strings.TrimSpace(c.Query("source"))
	search := strings.TrimSpace(c.Query("search"))
	files, _, err := h.collectFiles(c, conversationFilter, projectFilter)
	if err != nil {
		h.logger.Warn("failed to export chat attachments", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	files = filterChatUploadItems(files, sourceFilter, search)
	if len(files) == 0 {
		respondError(c, http.StatusNotFound, "没有可导出的文件", "no files to export")
		return
	}
	nameParts := []string{"chat-files"}
	if projectFilter != "" {
		nameParts = append(nameParts, "project-"+projectFilter)
	}
	if conversationFilter != "" {
		nameParts = append(nameParts, "conversation-"+conversationFilter)
	}
	nameParts = append(nameParts, time.Now().Format("20060102-150405"))
	filename := strings.Join(nameParts, "-") + ".zip"
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))

	zw := zip.NewWriter(c.Writer)
	defer zw.Close()

	manifest := gin.H{
		"exportedAt":      time.Now().UTC().Format(time.RFC3339),
		"conversationId":  conversationFilter,
		"projectId":       projectFilter,
		"source":          sourceFilter,
		"search":          search,
		"fileCount":       len(files),
		"files":           files,
		"layout":          "chat uploads are stored under conversations/<conversationId>/; internal outputs are stored under internal/<source>/",
		"sourceDirectory": []string{chatUploadsRootDirName, reductionRootDirName, workspaceRootDirName, artifactsRootDirName},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	mw, err := zw.Create("manifest.json")
	if err != nil {
		h.logger.Warn("failed to write the attachment export manifest", zap.Error(err))
		return
	}
	_, _ = mw.Write(manifestBytes)

	used := make(map[string]int)
	for _, item := range files {
		abs, err := h.resolveListedFilePath(item)
		if err != nil {
			continue
		}
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			continue
		}
		var zipName string
		if item.Source == chatUploadSourceReduction || strings.HasPrefix(item.RelativePath, reductionVirtualPrefix) {
			rel := strings.TrimPrefix(item.RelativePath, reductionVirtualPrefix)
			zipName = filepath.ToSlash(filepath.Join("internal", "reduction", rel))
			if filepath.Ext(zipName) == "" {
				zipName += ".txt"
			}
		} else if item.Source == chatUploadSourceWorkspace || strings.HasPrefix(item.RelativePath, workspaceVirtualPrefix) {
			rel := strings.TrimPrefix(item.RelativePath, workspaceVirtualPrefix)
			zipName = filepath.ToSlash(filepath.Join("internal", "workspace", rel))
		} else if item.Source == chatUploadSourceConversation || strings.HasPrefix(item.RelativePath, artifactVirtualPrefix) {
			rel := strings.TrimPrefix(item.RelativePath, artifactVirtualPrefix)
			zipName = filepath.ToSlash(filepath.Join("internal", "conversation_artifacts", rel))
			if filepath.Ext(zipName) == "" {
				zipName += ".txt"
			}
		} else {
			conv := strings.TrimSpace(item.ConversationID)
			if conv == "" || conv == "_manual" || conv == "_new" {
				conv = "manual"
			}
			zipName = filepath.ToSlash(filepath.Join("conversations", conv, strings.TrimSpace(item.SubPath), item.Name))
		}
		if used[zipName] > 0 {
			ext := filepath.Ext(zipName)
			zipName = strings.TrimSuffix(zipName, ext) + fmt.Sprintf("-%d", used[zipName]+1) + ext
		}
		used[zipName]++
		fw, err := zw.Create(zipName)
		if err != nil {
			h.logger.Warn("failed to create the attachment export entry", zap.String("path", item.RelativePath), zap.Error(err))
			continue
		}
		src, err := os.Open(abs)
		if err != nil {
			continue
		}
		_, copyErr := io.Copy(fw, src)
		_ = src.Close()
		if copyErr != nil {
			h.logger.Warn("failed to copy the attachment export entry", zap.String("path", item.RelativePath), zap.Error(copyErr))
			return
		}
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "file", "export", "chat attachments exported", "chat_upload", filename, map[string]interface{}{
			"conversation_id": conversationFilter,
			"project_id":      projectFilter,
			"file_count":      len(files),
		})
	}
}

// Download GET /api/chat-uploads/download?path=...
func (h *ChatUploadsHandler) Download(c *gin.Context) {
	p := c.Query("path")
	if strings.HasPrefix(strings.TrimSpace(p), reductionVirtualPrefix) {
		if !h.reductionVirtualPathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err := h.resolveReductionVirtualPath(p)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		name := filepath.Base(abs)
		if filepath.Ext(name) == "" {
			name += ".txt"
		}
		c.FileAttachment(abs, name)
		return
	}
	if strings.HasPrefix(strings.TrimSpace(p), workspaceVirtualPrefix) {
		if !h.workspaceVirtualPathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err := h.resolveWorkspaceVirtualPath(p)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		c.FileAttachment(abs, filepath.Base(abs))
		return
	}
	if strings.HasPrefix(strings.TrimSpace(p), artifactVirtualPrefix) {
		if !h.conversationArtifactVirtualPathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err := h.resolveConversationArtifactVirtualPath(p)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		name := filepath.Base(abs)
		if filepath.Ext(name) == "" {
			name += ".txt"
		}
		c.FileAttachment(abs, name)
		return
	}
	if !h.pathAllowed(c, p) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	abs, err := h.resolveUnderChatUploads(p)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		respondError(c, http.StatusNotFound, "文件不存在", "file not found")
		return
	}
	c.FileAttachment(abs, filepath.Base(abs))
}

// ResolvePath GET /api/chat-uploads/path?path=...&kind=file|directory
func (h *ChatUploadsHandler) ResolvePath(c *gin.Context) {
	p := strings.TrimSpace(c.Query("path"))
	kind := strings.TrimSpace(c.Query("kind"))
	if kind == "" {
		kind = "file"
	}
	var abs string
	var err error
	switch {
	case strings.HasPrefix(p, reductionVirtualPrefix):
		if strings.Trim(strings.TrimPrefix(p, reductionVirtualPrefix), "/") == "" {
			if _, ok := security.CurrentSession(c); !ok {
				respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
				return
			}
			abs, err = h.absReductionRoot()
			break
		}
		if !h.reductionVirtualPathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err = h.resolveReductionVirtualPath(p)
	case strings.HasPrefix(p, workspaceVirtualPrefix):
		if strings.Trim(strings.TrimPrefix(p, workspaceVirtualPrefix), "/") == "" {
			if _, ok := security.CurrentSession(c); !ok {
				respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
				return
			}
			abs, err = h.absWorkspaceRoot()
			break
		}
		if !h.workspaceVirtualPathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err = h.resolveWorkspaceVirtualPath(p)
	case strings.HasPrefix(p, artifactVirtualPrefix):
		if strings.Trim(strings.TrimPrefix(p, artifactVirtualPrefix), "/") == "" {
			if _, ok := security.CurrentSession(c); !ok {
				respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
				return
			}
			abs, err = h.absConversationArtifactsRoot()
			break
		}
		if !h.conversationArtifactVirtualPathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err = h.resolveConversationArtifactVirtualPath(p)
	default:
		if p == "" || p == "." {
			if _, ok := security.CurrentSession(c); !ok {
				respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
				return
			}
			abs, err = h.absRoot()
			break
		}
		if !h.pathAllowed(c, p) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		abs, err = h.resolveUnderChatUploads(p)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		respondError(c, http.StatusNotFound, "路径不存在", "path not found")
		return
	}
	if kind == "directory" && !st.IsDir() {
		respondError(c, http.StatusBadRequest, "不是目录", "not a directory")
		return
	}
	if kind != "directory" && st.IsDir() {
		respondError(c, http.StatusBadRequest, "不是文件", "not a file")
		return
	}
	c.JSON(http.StatusOK, gin.H{"absolutePath": abs, "isDir": st.IsDir()})
}

type chatUploadPathBody struct {
	Path string `json:"path"`
}

// Delete DELETE /api/chat-uploads
func (h *ChatUploadsHandler) Delete(c *gin.Context) {
	var body chatUploadPathBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		respondError(c, http.StatusBadRequest, "无效的请求体", "invalid body")
		return
	}
	if !h.pathAllowed(c, body.Path) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	abs, err := h.resolveUnderChatUploads(body.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if st.IsDir() {
		if err := os.RemoveAll(abs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if err := os.Remove(abs); err != nil {
			if os.IsNotExist(err) {
				respondError(c, http.StatusNotFound, "文件不存在", "file not found")
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	_ = h.db.DeleteChatUploadArtifactPath(filepath.ToSlash(filepath.Clean(filepath.FromSlash(body.Path))))
	if h.audit != nil {
		h.audit.RecordOK(c, "file", "delete", "chat attachment deleted", "chat_upload", body.Path, nil)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type chatUploadMkdirBody struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

// Mkdir POST /api/chat-uploads/mkdir — 在 parent 目录下新建子目录（parent 为 chat_uploads 下相对路径，空表示根目录；name 为单段目录名）
func (h *ChatUploadsHandler) Mkdir(c *gin.Context) {
	var body chatUploadMkdirBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求体", "invalid body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		respondError(c, http.StatusBadRequest, "名称无效", "invalid name")
		return
	}
	if utf8.RuneCountInString(name) > 200 {
		respondError(c, http.StatusBadRequest, "名称过长", "name too long")
		return
	}

	parent := strings.TrimSpace(body.Parent)
	parent = filepath.ToSlash(filepath.Clean(filepath.FromSlash(parent)))
	parent = strings.Trim(parent, "/")
	if parent == "." {
		parent = ""
	}
	if !h.pathAllowed(c, parent) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}

	root, err := h.absRoot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if parent != "" {
		absParent, err := h.resolveUnderChatUploads(parent)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := os.Stat(absParent)
		if err != nil || !st.IsDir() {
			respondError(c, http.StatusBadRequest, "父目录不存在", "parent not found")
			return
		}
	}

	var rel string
	if parent == "" {
		rel = name
	} else {
		rel = parent + "/" + name
	}
	absNew, err := h.resolveUnderChatUploads(rel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(absNew); err == nil {
		respondError(c, http.StatusConflict, "已存在", "already exists")
		return
	}
	if err := os.Mkdir(absNew, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	relOut, _ := filepath.Rel(root, absNew)
	c.JSON(http.StatusOK, gin.H{"ok": true, "relativePath": filepath.ToSlash(relOut)})
}

type chatUploadRenameBody struct {
	Path    string `json:"path"`
	NewName string `json:"newName"`
}

// Rename PUT /api/chat-uploads/rename
func (h *ChatUploadsHandler) Rename(c *gin.Context) {
	var body chatUploadRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求体", "invalid body")
		return
	}
	if !h.pathAllowed(c, body.Path) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	newName := strings.TrimSpace(body.NewName)
	if newName == "" || strings.ContainsAny(newName, `/\`) {
		respondError(c, http.StatusBadRequest, "newName 无效", "invalid newName")
		return
	}
	abs, err := h.resolveUnderChatUploads(body.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dir := filepath.Dir(abs)
	newAbs := filepath.Join(dir, filepath.Base(newName))
	root, _ := h.absRoot()
	newAbs, _ = filepath.Abs(newAbs)
	if newAbs != root && !strings.HasPrefix(newAbs, root+string(filepath.Separator)) {
		respondError(c, http.StatusBadRequest, "目标路径无效", "invalid target path")
		return
	}
	if err := os.Rename(abs, newAbs); err != nil {
		if os.IsNotExist(err) {
			respondError(c, http.StatusNotFound, "文件不存在", "file not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	newRel, _ := filepath.Rel(root, newAbs)
	oldRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(body.Path)))
	_ = h.db.RenameChatUploadArtifactPath(oldRel, filepath.ToSlash(newRel))
	c.JSON(http.StatusOK, gin.H{"ok": true, "relativePath": filepath.ToSlash(newRel)})
}

type chatUploadContentBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// GetContent GET /api/chat-uploads/content?path=...
func (h *ChatUploadsHandler) GetContent(c *gin.Context) {
	p := c.Query("path")
	if !h.pathAllowed(c, p) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	abs, err := h.resolveUnderChatUploads(p)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		respondError(c, http.StatusNotFound, "文件不存在", "file not found")
		return
	}
	if st.Size() > maxChatUploadEditBytes {
		respondError(c, http.StatusRequestEntityTooLarge, "文件过大，无法在编辑器打开", "file too large for editor")
		return
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !utf8.Valid(b) {
		respondError(c, http.StatusBadRequest, "二进制文件无法在 UI 编辑", "binary file not editable in UI")
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": string(b)})
}

// PutContent PUT /api/chat-uploads/content
func (h *ChatUploadsHandler) PutContent(c *gin.Context) {
	var body chatUploadContentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求体", "invalid body")
		return
	}
	if !h.pathAllowed(c, body.Path) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	if !utf8.ValidString(body.Content) {
		respondError(c, http.StatusBadRequest, "content 必须是合法 UTF-8", "content must be valid UTF-8")
		return
	}
	if len(body.Content) > maxChatUploadEditBytes {
		respondError(c, http.StatusRequestEntityTooLarge, "content 过长", "content too large")
		return
	}
	abs, err := h.resolveUnderChatUploads(body.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := os.WriteFile(abs, []byte(body.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func chatUploadShortRand(n int) string {
	const letters = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

// Upload POST /api/chat-uploads multipart: file；conversationId 可选；relativeDir 可选（chat_uploads 下目录的相对路径，将文件直接上传至该目录）
func (h *ChatUploadsHandler) Upload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		respondError(c, http.StatusBadRequest, "缺少文件", "missing file")
		return
	}
	root, err := h.absRoot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var targetDir string
	targetRel := strings.TrimSpace(c.PostForm("relativeDir"))
	if targetRel != "" {
		if !h.pathAllowed(c, targetRel) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		absDir, err := h.resolveUnderChatUploads(targetRel)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := os.Stat(absDir)
		if err != nil {
			if os.IsNotExist(err) {
				if err := os.MkdirAll(absDir, 0755); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		} else if !st.IsDir() {
			respondError(c, http.StatusBadRequest, "relativeDir 不是目录", "relativeDir is not a directory")
			return
		}
		targetDir = absDir
	} else {
		convID := strings.TrimSpace(c.PostForm("conversationId"))
		dateStr := time.Now().Format("2006-01-02")
		if !h.pathAllowed(c, filepath.ToSlash(filepath.Join(dateStr, convID))) {
			respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
			return
		}
		convDir := convID
		if convDir == "" {
			convDir = "_manual"
		} else {
			convDir = strings.ReplaceAll(convDir, string(filepath.Separator), "_")
		}
		targetDir = filepath.Join(root, dateStr, convDir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	baseName := filepath.Base(fh.Filename)
	if baseName == "" || baseName == "." {
		baseName = "file"
	}
	baseName = strings.ReplaceAll(baseName, string(filepath.Separator), "_")
	ext := filepath.Ext(baseName)
	nameNoExt := strings.TrimSuffix(baseName, ext)
	suffix := fmt.Sprintf("_%s_%s", time.Now().Format("150405"), chatUploadShortRand(6))
	var unique string
	if ext != "" {
		unique = nameNoExt + suffix + ext
	} else {
		unique = baseName + suffix
	}
	fullPath := filepath.Join(targetDir, unique)
	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()
	dst, err := os.Create(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(fullPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rel, _ := filepath.Rel(root, fullPath)
	absSaved, _ := filepath.Abs(fullPath)
	if session, ok := security.CurrentSession(c); ok {
		conversationID := strings.TrimSpace(c.PostForm("conversationId"))
		if conversationID == "" {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) >= 2 {
				conversationID = parts[1]
			}
		}
		_ = h.db.UpsertChatUploadArtifact(filepath.ToSlash(rel), conversationID, session.UserID)
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "file", "upload", "chat attachment uploaded", "chat_upload", filepath.ToSlash(rel), map[string]interface{}{
			"name": unique,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"relativePath": filepath.ToSlash(rel),
		"absolutePath": absSaved,
		"name":         unique,
	})
}
