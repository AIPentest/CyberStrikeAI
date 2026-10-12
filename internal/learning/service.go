package learning

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

type Service struct {
	db          *database.DB
	cfg         config.LearningConfig
	extractor   Extractor
	knownSecret string
	logger      *zap.Logger
	cancel      context.CancelFunc
	done        chan struct{}
	stopOnce    sync.Once
}

func New(db *database.DB, cfg config.LearningConfig, extractor Extractor, knownSecret string, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{db: db, cfg: cfg, extractor: extractor, knownSecret: knownSecret, logger: logger, done: make(chan struct{})}
}

func (s *Service) Start() error {
	if err := s.db.RecoverLearningRuns(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			if err := s.ProcessOne(ctx, time.Now()); err != nil {
				s.logger.Warn("经验学习作业处理失败")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
	})
}

func (s *Service) ProcessOne(ctx context.Context, now time.Time) error {
	if err := s.db.PruneLearningJobs(now); err != nil {
		return err
	}
	j, err := s.db.ClaimLearningJob(now, s.cfg.DailyLimit(), time.Duration(s.cfg.Timeout()+30)*time.Second)
	if err != nil || j == nil {
		return err
	}
	if !s.db.LearningSourceValid(j) {
		return s.db.CompleteLearningJob(j, "skipped", "", "", "来源已删除、范围变更或权限失效", 0, 0)
	}
	evidence, err := s.db.LearningRunEvidence(j, s.cfg.InputLimit())
	if err != nil {
		return s.db.RetryLearningJob(j, "读取证据失败", now)
	}
	evidence = SanitizeEvidence(evidence, s.knownSecret)
	hasTool := false
	for _, e := range evidence {
		hasTool = hasTool || e.Kind == "tool_result"
	}
	if !hasTool {
		return s.db.CompleteLearningJob(j, "skipped", "", "", "缺少可提炼的工具执行证据", 0, 0)
	}
	jobCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Timeout())*time.Second)
	r, err := s.extractor.Extract(jobCtx, j.RunStatus, evidence)
	cancel()
	if usageErr := s.db.RecordLearningUsage(j, r.PromptTokens, r.CompletionTokens); usageErr != nil {
		return usageErr
	}
	if err != nil {
		return s.db.RetryLearningJob(j, "模型提炼失败或返回格式无效", now)
	}
	if s.knownSecret != "" {
		for _, text := range []string{r.Candidate.Name, r.Candidate.Description, r.Candidate.Body, r.Candidate.Reason, r.Candidate.SkillMarkdown} {
			if strings.Contains(text, s.knownSecret) {
				return s.db.RetryLearningJob(j, "草稿包含受保护的配置值", now)
			}
		}
	}
	hash, err := ValidateCandidate(&r.Candidate, j.RunStatus, evidence)
	if err != nil {
		return s.db.RetryLearningJob(j, "草稿校验失败，未保存候选正文", now)
	}
	status, candidate := "skipped", ""
	if r.Candidate.Decision != "skip" {
		status = "drafted"
		b, _ := json.Marshal(r.Candidate)
		candidate = string(b)
	}
	return s.db.CompleteLearningJob(j, status, candidate, hash, r.Candidate.Reason, 0, 0)
}
