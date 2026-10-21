package multiagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// newEinoAgenticSummarizationMiddleware wires the project's domain-specific
// compaction policy into Eino's native typed AgenticMessage summarization.
func newEinoAgenticSummarizationMiddleware(
	ctx context.Context,
	summaryModel model.BaseModel[*schema.AgenticMessage],
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	conversationID string,
	db *database.DB,
	projectID string,
	logger *zap.Logger,
	memoryScopes ...string,
) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	if summaryModel == nil || appCfg == nil {
		return nil, fmt.Errorf("multiagent: agentic summarization 需要 model 与配置")
	}
	maxTotal := appCfg.OpenAI.MaxTotalTokens
	if maxTotal <= 0 {
		maxTotal = 120000
	}
	triggerRatio := 0.8
	emitInternalEvents := true
	outputReserve := config.DefaultSummarizationOutputReserveTokens
	userLedgerMaxRunes := config.DefaultSummarizationUserIntentLedgerMaxRunes
	userLedgerEntryMaxRunes := config.DefaultSummarizationUserIntentLedgerEntryMaxRunes
	toolMaxBytes := config.MultiAgentEinoMiddlewareConfig{}.ReductionMaxLengthForTruncEffective()
	if mwCfg != nil {
		triggerRatio = mwCfg.SummarizationTriggerRatioEffective()
		emitInternalEvents = mwCfg.SummarizationEmitInternalEventsEffective()
		outputReserve = mwCfg.SummarizationOutputReserveTokensEffective()
		userLedgerMaxRunes = mwCfg.SummarizationUserIntentLedgerMaxRunesEffective()
		userLedgerEntryMaxRunes = mwCfg.SummarizationUserIntentLedgerEntryMaxRunesEffective()
		toolMaxBytes = mwCfg.ReductionMaxLengthForTruncEffective()
	}

	ledgerWindowCap := modelFacingRuneBudget(maxTotal, 0.20)
	userLedgerMaxRunes = minPositiveInt(userLedgerMaxRunes, ledgerWindowCap)
	userLedgerEntryMaxRunes = minPositiveInt(userLedgerEntryMaxRunes, userLedgerMaxRunes)
	trigger := int(float64(maxTotal) * triggerRatio)
	if trigger < 4096 {
		trigger = maxTotal
		if trigger < 4096 {
			trigger = 4096
		}
	}
	modelName := strings.TrimSpace(appCfg.OpenAI.Model)
	if modelName == "" {
		modelName = "gpt-4o"
	}
	classicTokenCounter := einoSummarizationTokenCounterForChannel(modelName, appCfg.OpenAI)
	agenticTokenCounter := func(ctx context.Context, input *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
		if input == nil {
			return 0, nil
		}
		return classicTokenCounter(ctx, &summarization.TokenCounterInput{
			Messages: AgenticMessagesToEino(input.Messages),
			Tools:    input.Tools,
		})
	}
	recentTrailMax := trigger / 4
	if recentTrailMax < 2048 {
		recentTrailMax = 2048
	}
	if recentTrailMax > trigger/2 {
		recentTrailMax = trigger / 2
	}
	summaryInputMax := trigger - outputReserve
	if summaryInputMax < 4096 {
		summaryInputMax = trigger * 80 / 100
	}
	if summaryInputMax < 4096 {
		summaryInputMax = 4096
	}

	memoryScope := summarizationMemoryScope(memoryScopes)
	transcriptPath := ""
	if conv := strings.TrimSpace(conversationID); conv != "" {
		baseRoot := filepath.Join(os.TempDir(), "cyberstrike-summarization", sanitizeEinoPathSegment(conv), "summarization")
		if dbPath := strings.TrimSpace(appCfg.Database.Path); dbPath != "" {
			baseRoot = filepath.Join(filepath.Dir(dbPath), "conversation_artifacts", sanitizeEinoPathSegment(conv), "summarization")
		}
		if memoryScope != "main" {
			baseRoot = filepath.Join(baseRoot, "scopes", summarizationScopeID(memoryScope))
		}
		base := baseRoot
		if abs, err := filepath.Abs(base); err == nil {
			base = abs
		}
		if mkErr := os.MkdirAll(base, 0o755); mkErr == nil {
			transcriptPath = filepath.Join(base, "transcript.txt")
		}
	}

	retryPolicy := einoTransientRunRetryPolicyFromMW(mwCfg)
	retryMax := retryPolicy.maxAttempts
	var summaryOverflowRetries int
	summaryModelOpts := newEinoSummarizationModelOptions(outputReserve, modelName, "agentic", &appCfg.OpenAI, logger)

	mw, err := summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model:        newNonEmptyAgenticSummaryModel(summaryModel),
		ModelOptions: summaryModelOpts,
		GenModelInput: func(ctx context.Context, sysInstruction, userInstruction *schema.AgenticMessage, originalMsgs []*schema.AgenticMessage) ([]*schema.AgenticMessage, error) {
			classicOriginal := AgenticMessagesToEino(originalMsgs)
			if transcriptPath != "" && len(classicOriginal) > 0 {
				if werr := writeSummarizationTranscript(transcriptPath, classicOriginal); werr != nil && logger != nil {
					logger.Warn("eino agentic summarization transcript preflight 写入失败",
						zap.String("path", transcriptPath), zap.Error(werr))
				}
			}
			budget := summaryInputMax
			aggressive := summaryOverflowRetries > 0
			if aggressive {
				budget = summaryInputMax * 70 / 100
				if budget < 4096 {
					budget = 4096
				}
			}
			input, dropped, berr := buildBudgetedSummarizationModelInput(
				ctx,
				agenticInstructionToClassic(sysInstruction, schema.System),
				agenticInstructionToClassic(userInstruction, schema.User),
				classicOriginal,
				classicTokenCounter,
				budget,
				summarizationInputBudgetOpts{
					toolMaxBytes: toolMaxBytes,
					spillRef:     transcriptPath,
					aggressive:   aggressive,
				},
			)
			if logger != nil && (berr != nil || dropped > 0 || aggressive) {
				fields := []zap.Field{
					zap.Int("max_input_tokens", budget),
					zap.Int("trigger_context_tokens", trigger),
					zap.Int("output_reserve_tokens", outputReserve),
					zap.Int("dropped_rounds", dropped),
					zap.Bool("aggressive", aggressive),
				}
				if berr != nil {
					fields = append(fields, zap.Error(berr))
					logger.Warn("eino agentic summarization input budget failed", fields...)
				} else {
					logger.Info("eino agentic summarization input bounded", fields...)
				}
			}
			return EinoMessagesToAgentic(input), berr
		},
		Trigger: &summarization.TriggerCondition{
			ContextTokens: trigger,
		},
		TokenCounter:       agenticTokenCounter,
		UserInstruction:    einoSummarizeUserInstruction,
		EmitInternalEvents: emitInternalEvents,
		// Finalize advertises only a successfully written immutable snapshot.
		TranscriptFilePath: "",
		Retry: &summarization.TypedRetryConfig[*schema.AgenticMessage]{
			MaxRetries: &retryMax,
			ShouldRetry: func(_ context.Context, _ *schema.AgenticMessage, err error) bool {
				if isEinoContextOverflowError(err) && summaryOverflowRetries < 1 {
					summaryOverflowRetries++
					if logger != nil {
						logger.Warn("eino agentic summarization context overflow, retrying with aggressive compaction",
							zap.Error(err),
						)
					}
					return true
				}
				retry := isEinoTransientRunError(err)
				if retry && logger != nil {
					logger.Warn("eino agentic summarization generate transient error, will retry if attempts remain",
						zap.Error(err),
						zap.Int("max_retries", retryMax),
					)
				}
				return retry
			},
		},
		Finalize: func(ctx context.Context, originalMessages []*schema.AgenticMessage, summary *schema.AgenticMessage) ([]*schema.AgenticMessage, error) {
			classicOriginal := AgenticMessagesToEino(originalMessages)
			classicSummary := agenticSummaryToClassicMessage(summary)
			if classicSummary == nil {
				return nil, fmt.Errorf("agentic summarization returned empty summary")
			}
			compactionMessages := stripOriginalUserIntentLedgerFromMessages(classicOriginal)
			defaultFinalized, derr := summarization.DefaultFinalize(ctx, summarizationDefaultFinalizeInput(compactionMessages, classicOriginal), classicSummary)
			if derr != nil {
				return nil, derr
			}
			if len(defaultFinalized) == 0 {
				return nil, fmt.Errorf("agentic summarization default finalize returned no messages")
			}
			summaryMsg := finalizeSummarizationMemory(defaultFinalized[len(defaultFinalized)-1], classicOriginal, transcriptPath, db, projectID, conversationID, memoryScope, appCfg.Project, logger)
			userLedger := buildOriginalUserIntentLedgerMessage(classicOriginal, userLedgerMaxRunes, userLedgerEntryMaxRunes)
			out, ferr := summarizeFinalizeWithRecentAssistantToolTrail(ctx, compactionMessages, summaryMsg, classicTokenCounter, recentTrailMax)
			if ferr != nil {
				return nil, ferr
			}
			out = mergeMessageIntoLeadingSystem(out, userLedger)
			if appCfg != nil {
				out = refreshFactIndexInMessages(out, db, projectID, appCfg.Project, logger, conversationID)
			}
			return EinoMessagesToAgentic(out), nil
		},
		Callback: func(ctx context.Context, before, after adk.TypedChatModelAgentState[*schema.AgenticMessage]) error {
			classicBefore := AgenticMessagesToEino(before.Messages)
			classicAfter := AgenticMessagesToEino(after.Messages)
			if transcriptPath != "" && len(classicBefore) > 0 {
				if werr := writeSummarizationTranscript(transcriptPath, classicBefore); werr != nil && logger != nil {
					logger.Warn("eino agentic summarization transcript 写入失败",
						zap.String("path", transcriptPath),
						zap.Error(werr),
					)
				}
			}
			if logger != nil {
				beforeTokens, _ := classicTokenCounter(ctx, &summarization.TokenCounterInput{Messages: classicBefore})
				afterTokens, _ := classicTokenCounter(ctx, &summarization.TokenCounterInput{Messages: classicAfter})
				logger.Info("eino agentic summarization 已压缩上下文",
					zap.Int("messages_before", len(before.Messages)),
					zap.Int("messages_after", len(after.Messages)),
					zap.Int("tokens_before_estimated", beforeTokens),
					zap.Int("tokens_after_estimated", afterTokens),
					zap.Int("max_total_tokens", maxTotal),
					zap.Int("trigger_context_tokens", trigger),
					zap.String("transcript_file", transcriptPath),
				)
			}
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization.NewTyped[AgenticMessage]: %w", err)
	}
	return mw, nil
}

func agenticInstructionToClassic(msg *schema.AgenticMessage, fallbackRole schema.RoleType) *schema.Message {
	msgs := AgenticMessageToEino(msg)
	if len(msgs) > 0 && msgs[0] != nil {
		return msgs[0]
	}
	return &schema.Message{Role: fallbackRole}
}

func agenticSummaryToClassicMessage(msg *schema.AgenticMessage) *schema.Message {
	msgs := AgenticMessageToEino(msg)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == schema.Assistant || strings.TrimSpace(m.Content) != "" || m.ReasoningContent != "" {
			if m.Role != schema.Assistant {
				cp := *m
				cp.Role = schema.Assistant
				return &cp
			}
			return m
		}
	}
	return nil
}
