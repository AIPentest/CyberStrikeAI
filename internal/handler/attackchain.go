package handler

import (
	"fmt"
	"context"
	"net/http"
	"sync"
	"time"

	"cyberstrike-ai/internal/attackchain"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AttackChainHandler 攻击链处理器
type AttackChainHandler struct {
	db           *database.DB
	logger       *zap.Logger
	openAIConfig *config.OpenAIConfig
	mu           sync.RWMutex // 保护 openAIConfig 的并发访问
	// 用于防止同一对话的并发生成
	generatingLocks sync.Map // map[string]*sync.Mutex
}

// NewAttackChainHandler 创建新的攻击链处理器
func NewAttackChainHandler(db *database.DB, openAIConfig *config.OpenAIConfig, logger *zap.Logger) *AttackChainHandler {
	return &AttackChainHandler{
		db:           db,
		logger:       logger,
		openAIConfig: openAIConfig,
	}
}

// UpdateConfig 更新OpenAI配置
func (h *AttackChainHandler) UpdateConfig(cfg *config.OpenAIConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openAIConfig = cfg
	h.logger.Info("AttackChainHandler配置已更新",
		zap.String("base_url", cfg.BaseURL),
		zap.String("model", cfg.Model),
	)
}

// getOpenAIConfig 获取OpenAI配置（线程安全）
func (h *AttackChainHandler) getOpenAIConfig() *config.OpenAIConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.openAIConfig
}

// GetAttackChain 获取攻击链（按需生成）
// GET /api/attack-chain/:conversationId
func (h *AttackChainHandler) GetAttackChain(c *gin.Context) {
	conversationID := c.Param("conversationId")
	if conversationID == "" {
		respondError(c, http.StatusBadRequest, "conversationId 必填", "conversationId is required")
		return
	}

	// 检查对话是否存在
	_, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Warn("对话不存在", zap.String("conversationId", conversationID), zap.Error(err))
		respondError(c, http.StatusNotFound, "对话不存在", "conversation not found")
		return
	}

	// 先尝试从数据库加载（如果已生成过）
	openAIConfig := h.getOpenAIConfig()
	builder := attackchain.NewBuilder(h.db, openAIConfig, h.logger)
	chain, err := builder.LoadCachedChainIfPresent(conversationID)
	if err == nil && chain != nil {
		// 如果已存在，直接返回
		h.logger.Info("返回已存在的攻击链", zap.String("conversationId", conversationID))
		c.JSON(http.StatusOK, chain)
		return
	}

	// 如果不存在，则生成新的攻击链（按需生成）
	// 使用锁机制防止同一对话的并发生成
	lockInterface, _ := h.generatingLocks.LoadOrStore(conversationID, &sync.Mutex{})
	lock := lockInterface.(*sync.Mutex)

	// 尝试获取锁，如果正在生成则返回错误
	acquired := lock.TryLock()
	if !acquired {
		h.logger.Info("攻击链正在生成中，请稍后再试", zap.String("conversationId", conversationID))
		respondError(c, http.StatusConflict, "攻击链正在生成中，请稍后再试", "attack chain is being generated; try again later")
		return
	}
	defer lock.Unlock()

	// 再次检查是否已生成（可能在等待锁的过程中已经生成完成）
	chain, err = builder.LoadCachedChainIfPresent(conversationID)
	if err == nil && chain != nil {
		h.logger.Info("返回已存在的攻击链（在锁等待期间已生成）", zap.String("conversationId", conversationID))
		c.JSON(http.StatusOK, chain)
		return
	}

	h.logger.Info("开始生成攻击链", zap.String("conversationId", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	chain, err = builder.BuildChainFromConversation(ctx, conversationID)
	if err != nil {
		h.logger.Error("生成攻击链失败", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "生成攻击链失败: %s", "failed to generate attack chain: %s"), err.Error())})
		return
	}

	// 生成完成后，从锁映射中删除（可选，保留也可以用于防止短时间内重复生成）
	// h.generatingLocks.Delete(conversationID)

	c.JSON(http.StatusOK, chain)
}

// RegenerateAttackChain 重新生成攻击链
// POST /api/attack-chain/:conversationId/regenerate
func (h *AttackChainHandler) RegenerateAttackChain(c *gin.Context) {
	conversationID := c.Param("conversationId")
	if conversationID == "" {
		respondError(c, http.StatusBadRequest, "conversationId 必填", "conversationId is required")
		return
	}

	// 检查对话是否存在
	_, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Warn("对话不存在", zap.String("conversationId", conversationID), zap.Error(err))
		respondError(c, http.StatusNotFound, "对话不存在", "conversation not found")
		return
	}

	// 使用锁机制防止并发生成
	lockInterface, _ := h.generatingLocks.LoadOrStore(conversationID, &sync.Mutex{})
	lock := lockInterface.(*sync.Mutex)

	acquired := lock.TryLock()
	if !acquired {
		h.logger.Info("攻击链正在生成中，请稍后再试", zap.String("conversationId", conversationID))
		respondError(c, http.StatusConflict, "攻击链正在生成中，请稍后再试", "attack chain is being generated; try again later")
		return
	}
	defer lock.Unlock()

	// 只有持锁的生成请求可以删除旧图；返回 409 的请求不得修改缓存。
	if err := h.db.DeleteAttackChain(conversationID); err != nil {
		h.logger.Warn("删除旧攻击链失败", zap.Error(err))
	}

	// 生成新的攻击链
	h.logger.Info("重新生成攻击链", zap.String("conversationId", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	openAIConfig := h.getOpenAIConfig()
	builder := attackchain.NewBuilder(h.db, openAIConfig, h.logger)
	chain, err := builder.BuildChainFromConversation(ctx, conversationID)
	if err != nil {
		h.logger.Error("生成攻击链失败", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "生成攻击链失败: %s", "failed to generate attack chain: %s"), err.Error())})
		return
	}

	c.JSON(http.StatusOK, chain)
}
