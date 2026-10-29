package app

import (
	"context"
	"strings"

	"cyberstrike-ai/internal/assetintel"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// maxAutoIngestHosts 限制单次工具输出最多采集多少条主机记录，
// 防止一条超大输出（例如全端口扫描）把一次事务撑爆。
const maxAutoIngestHosts = 5000

// assetIngestor 把第三方信息收集工具的原始输出清洗成资产并落库。
//
// 触发方式是 agent.ToolResultHook：Agent 每次工具调用成功后会回调这里。
// 这样模型不需要逐条调用 create_asset，资产库也能自然沉淀扫描结果。
type assetIngestor struct {
	db     *database.DB
	logger *zap.Logger
}

func newAssetIngestor(db *database.DB, logger *zap.Logger) *assetIngestor {
	if db == nil || logger == nil {
		return nil
	}
	return &assetIngestor{db: db, logger: logger}
}

// Ingest 是 ToolResultHook 的实现。任何失败都只记日志，绝不向上抛错。
func (i *assetIngestor) Ingest(ctx context.Context, toolName string, args map[string]interface{}, output string) {
	if i == nil || i.db == nil {
		return
	}
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	if !assetintel.Supports(toolName) {
		return
	}

	hosts, err := assetintel.Parse(toolName, args, output)
	if err != nil {
		i.logger.Warn("资产自动采集：解析工具输出失败",
			zap.String("tool", toolName), zap.Error(err))
		return
	}
	if len(hosts) == 0 {
		i.logger.Debug("资产自动采集：未从输出中识别到主机", zap.String("tool", toolName))
		return
	}
	if len(hosts) > maxAutoIngestHosts {
		i.logger.Warn("资产自动采集：单次结果过多，已截断",
			zap.String("tool", toolName),
			zap.Int("total", len(hosts)),
			zap.Int("kept", maxAutoIngestHosts))
		hosts = hosts[:maxAutoIngestHosts]
	}

	_, ownerUserID, allowGlobal := assetAccessFromToolContext(ctx, "asset:write")
	projectID, _, scopeErr := agentAssetProjectScope(i.db, ctx)
	if scopeErr != nil {
		// 项目范围判断失败不阻断采集，仅按「未绑定项目」处理。
		i.logger.Warn("资产自动采集：无法确定会话项目范围", zap.Error(scopeErr))
		projectID = ""
	}
	conversationID := conversationIDFromToolCtx(ctx)

	source := "tool:" + toolName
	assets := make([]*database.Asset, 0, len(hosts))
	for _, h := range hosts {
		asset := &database.Asset{
			Host:     h.Host,
			IP:       h.IP,
			Port:     h.Port,
			Domain:   h.Domain,
			Protocol: h.Protocol,
			Title:    h.Title,
			Server:   h.Server,
			Source:   source,
			Status:   "active",
			Tags:     []string{"auto-collected", toolName},
		}
		if projectID != "" {
			asset.ProjectID = projectID
		}
		if conversationID != "" {
			asset.LastScanConversationID = conversationID
		}
		assets = append(assets, asset)
	}

	result, err := i.db.UpsertAssets(assets, ownerUserID, allowGlobal)
	if err != nil {
		i.logger.Warn("资产自动采集：写入资产失败",
			zap.String("tool", toolName), zap.Error(err))
		return
	}

	// UpsertAssets 会把落库后的资产 ID 回填到入参指针上，据此写入指纹。
	fingerprintCount := 0
	for idx, asset := range assets {
		if asset.ID == "" {
			continue
		}
		inputs := fingerprintInputsFor(hosts[idx])
		if len(inputs) == 0 {
			continue
		}
		if err := i.db.UpsertAssetFingerprints(asset.ID, inputs); err != nil {
			i.logger.Warn("资产自动采集：写入指纹失败",
				zap.String("tool", toolName),
				zap.String("asset_id", asset.ID),
				zap.Error(err))
			continue
		}
		fingerprintCount += len(inputs)
	}

	i.logger.Info("资产自动采集完成",
		zap.String("tool", toolName),
		zap.Int("parsed", len(hosts)),
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("skipped", result.Skipped),
		zap.Int("fingerprints", fingerprintCount),
		zap.String("project_id", projectID))
}

// fingerprintInputsFor 把一条解析结果转换成指纹行。
// 服务名与指纹分开存：service 来自 nmap -sV（如 ssh/http），
// technology/version 来自 httpx tech-detect 或 nmap product/version。
func fingerprintInputsFor(h assetintel.Host) []database.AssetFingerprintInput {
	inputs := make([]database.AssetFingerprintInput, 0, len(h.Technologies)+1)
	for _, tech := range h.Technologies {
		inputs = append(inputs, database.AssetFingerprintInput{
			Service:    h.Service,
			Technology: tech.Name,
			Version:    tech.Version,
		})
	}
	if len(inputs) == 0 && strings.TrimSpace(h.Service) != "" {
		inputs = append(inputs, database.AssetFingerprintInput{Service: h.Service})
	}
	return inputs
}
