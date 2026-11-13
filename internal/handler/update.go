package handler

import (
	"context"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/update"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// updateRestartDelay is how long a stand-down waits after the response is written: the
// page that asked for the restart gets one more poll window to read the final state
// before the process it is watching goes away. A variable so tests do not sleep for it.
var updateRestartDelay = 2 * time.Second

// UpdateHandler is the "update my own source" surface: what this installation is, what
// its remote has that it does not, and one action that moves the tree and rebuilds the
// binary.
//
// It exists because the alternative was a shell script pointed at one fixed upstream
// repository, which for anyone running a fork meant "downgrade yourself to somebody
// else's code". Here the remote is whatever this directory already tracks, and the person
// at the keyboard never types git or go build.
type UpdateHandler struct {
	root   string // the installation to update: the directory the config file lives in
	logger *zap.Logger
	audit  *audit.Service
	// prepareRestart validates the replacement before the running service shuts down.
	prepareRestart func(activationID string) (func(), error)
	bootID         string
	activationID   string
	buildRevision  string
	// source reports the configured update source (config.yaml update.repo) as the
	// operator last saved it; an empty string means "not configured" and the official
	// repository is used. A function rather than a value because the config can change
	// while the process runs.
	source func() string
	// saveSource persists a new source through the config layer; nil means this start
	// has no config to write to.
	saveSource func(repo string) error
	// baseline is the binary this process is actually running: the file on disk at
	// construction time. An update, a rollback or a CLI run that replaces it leaves a
	// different file behind, and that difference is what "restart to activate" means.
	baseline update.BinaryStamp

	mu     sync.Mutex
	jobs   map[string]*updateJob
	order  []string
	active string
}

// updateJob is one running or finished apply. A build takes minutes, so the request
// returns a handle and the page polls it rather than sitting in one HTTP call until the
// compiler finishes.
type updateJob struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`  // apply | adopt
	State        string         `json:"state"` // running | succeeded | failed
	Started      string         `json:"started"`
	Finished     string         `json:"finished,omitempty"`
	Steps        []update.Step  `json:"steps"`
	Result       *update.Result `json:"result,omitempty"`
	Failure      *update.Error  `json:"failure,omitempty"`
	Restart      bool           `json:"restartRequested"`
	RestartError string         `json:"restartError,omitempty"`
}

// view copies a job for transport, because its fields are written by the job's own
// goroutine: a response must never serialize the live struct. Callers hold h.mu.
func (j *updateJob) view() *updateJob {
	out := *j
	out.Steps = append([]update.Step{}, j.Steps...)
	return &out
}

// NewUpdateHandler takes the audit service as a constructor argument rather than through
// a SetAudit the assembly has to remember: forgetting here is a compile error instead of
// a privileged endpoint that writes no audit records at all.
func NewUpdateHandler(root string, logger *zap.Logger, auditSvc *audit.Service, prepareRestart func(string) (func(), error), activationID string,
	source func() string, saveSource func(repo string) error) *UpdateHandler {
	var revision string
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
				break
			}
		}
	}
	return &UpdateHandler{
		root:           root,
		logger:         logger,
		audit:          auditSvc,
		prepareRestart: prepareRestart,
		bootID:         uuid.NewString(),
		activationID:   activationID,
		buildRevision:  revision,
		source:         source,
		saveSource:     saveSource,
		baseline:       update.StampBinary(root, update.Options{Root: root}),
		jobs:           map[string]*updateJob{},
	}
}

// restartPending reports whether the binary on disk is no longer the one this process is
// running, and when that binary was built (empty when there is nothing on disk to run).
func (h *UpdateHandler) restartPending() (bool, string) {
	stamp := update.StampBinary(h.root, h.options())
	if stamp.Same(h.baseline) {
		return false, ""
	}
	builtAt := ""
	if stamp.Exists {
		builtAt = stamp.ModTime.Format(time.RFC3339)
	}
	return true, builtAt
}

// restartSupervised is a legacy status hint. These environment markers do not
// prove that the supervisor is configured to restart the service.
func restartSupervised() bool {
	return strings.TrimSpace(os.Getenv("XPC_SERVICE_NAME")) != "" ||
		strings.TrimSpace(os.Getenv("INVOCATION_ID")) != "" ||
		strings.TrimSpace(os.Getenv("JOURNAL_STREAM")) != ""
}

func (h *UpdateHandler) options() update.Options {
	opts := update.Options{Root: h.root}
	if h.source != nil {
		opts.Repo = h.source()
	}
	return opts
}

// sourceView is the configured source as the page edits it: the address (empty = the
// official repository) plus the default, so the page never hardcodes a URL the server
// would disagree with.
func (h *UpdateHandler) sourceView() gin.H {
	repo := ""
	if h.source != nil {
		repo = strings.TrimSpace(h.source())
	}
	return gin.H{
		"repo":        repo,
		"configured":  repo != "",
		"defaultRepo": update.DefaultRepoURL,
	}
}

// GetStatus answers GET /api/system/update: the install tree as it is on disk, without a
// network round trip, so opening the page is never gated on GitHub being reachable.
func (h *UpdateHandler) GetStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	snap, err := update.Status(c.Request.Context(), h.options())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	needsRestart, binaryBuiltAt := h.restartPending()
	c.JSON(http.StatusOK, gin.H{
		"status":             snap,
		"job":                h.latest(),
		"canRestart":         h.prepareRestart != nil,
		"autoRestart":        h.prepareRestart != nil,
		"bootID":             h.bootID,
		"buildRevision":      h.buildRevision,
		"activationTracking": true,
		"supervised":         restartSupervised(),
		"needsRestart":       needsRestart,
		"binaryBuiltAt":      binaryBuiltAt,
		"source":             h.sourceView(),
	})
}

// SetBootHeader runs before auth on the validation route so a client can tell
// a new boot from a revoked or expired session on the current boot.
func (h *UpdateHandler) SetBootHeader(c *gin.Context) {
	c.Header("X-CyberStrike-Boot-ID", h.bootID)
	c.Header("X-CyberStrike-Build-Revision", h.buildRevision)
	activationID := h.activationID
	if activationID == "" {
		activationID = "-"
	}
	c.Header("X-CyberStrike-Activation-ID", activationID)
	c.Header("Cache-Control", "no-store")
}

// Check answers POST /api/system/update/check: fetch the tracked branch and report the
// gap. A failed fetch is a 200 with checkError, not a 500 - "offline" and "bad token"
// are answers the operator needs to read, not server faults to debug.
func (h *UpdateHandler) Check(c *gin.Context) {
	snap, err := update.Check(c.Request.Context(), h.options())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.record(c, "update.check", resultOf(snap.CheckError), fmt.Sprintf("检查更新：%s（默认分支 %s）", snap.Repo, snap.TargetBranch), map[string]interface{}{
		"commit": snap.Commit, "remote_commit": snap.RemoteCommit, "behind": snap.Behind, "error": snap.CheckError,
	})
	c.JSON(http.StatusOK, gin.H{"status": snap})
}

// SaveSource answers POST /api/system/update/source: the operator picks which repository
// this installation updates from - the official one, a mirror, or their own fork - and
// the choice lands in the update section of config.yaml. An empty address clears the
// setting, which means the official repository again.
func (h *UpdateHandler) SaveSource(c *gin.Context) {
	var body struct {
		Repo string `json:"repo"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体不是合法 JSON"})
		return
	}
	body.Repo = strings.TrimSpace(body.Repo)
	if body.Repo != "" && !update.ValidRemoteURL(body.Repo) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仓库地址不合法（只允许 https/http/ssh/git/file:// 或本机绝对路径）"})
		return
	}
	if h.saveSource == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "本次启动没有接入配置保存，更新源改不了"})
		return
	}
	release, err := update.LockInstall(h.options())
	if err != nil {
		c.JSON(statusForError(err), errorBody(err))
		return
	}
	defer release()
	if busy := h.activeJob(); busy != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "更新进行中，不能切换更新源", "job": busy})
		return
	}
	if err := h.saveSource(body.Repo); err != nil {
		h.record(c, "update.source", "failure", "保存更新源失败: "+err.Error(), map[string]interface{}{"error": err.Error()})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败：" + err.Error()})
		return
	}
	message := "更新源已保存：" + body.Repo
	if body.Repo == "" {
		message = "已恢复默认更新源（官方仓库）"
	}
	h.record(c, "update.source", "success", message, map[string]interface{}{"repo": body.Repo})
	c.JSON(http.StatusOK, gin.H{"source": h.sourceView()})
}

// Adopt answers POST /api/system/update/adopt. Without confirm it is a side-effect-free
// preview (the fetch happens in a throwaway repository outside the install tree); with
// confirm it starts the same kind of job an update does.
func (h *UpdateHandler) Adopt(c *gin.Context) {
	var body struct {
		Confirm bool `json:"confirm"`
		Restart bool `json:"restart"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求体不是合法 JSON"})
			return
		}
	}
	if body.Confirm && body.Restart && h.prepareRestart == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "本次启动没有提供重启钩子，接入完成后请由外部进程管理器重启服务"})
		return
	}
	if !body.Confirm {
		plan, err := update.Preview(c.Request.Context(), h.options())
		if err != nil {
			h.record(c, "update.adopt", "failure", "接入预览失败: "+err.Error(), map[string]interface{}{"error": errMessage(err)})
			c.JSON(statusForError(err), errorBody(err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"plan": plan})
		return
	}
	job, started := h.startJob("adopt", body.Restart)
	if !started {
		c.JSON(http.StatusConflict, gin.H{"error": "已有一次更新在进行中", "job": job, "bootID": h.bootID})
		return
	}
	h.record(c, "update.adopt", "started", "开始把目录接入更新源", map[string]interface{}{"job": job.ID, "restart": body.Restart})
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "state": job.State, "bootID": h.bootID})
}

// Apply answers POST /api/system/update/apply. It starts one job and returns immediately;
// a second concurrent apply is refused, because two processes moving the same working
// tree and swapping the same binary is how an installation becomes unrecoverable.
func (h *UpdateHandler) Apply(c *gin.Context) {
	var body struct {
		Restart bool `json:"restart"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求体不是合法 JSON"})
			return
		}
	}
	if body.Restart && h.prepareRestart == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "本次启动没有提供重启钩子，更新完成后请由外部进程管理器重启服务"})
		return
	}

	job, started := h.startJob("apply", body.Restart)
	if !started {
		c.JSON(http.StatusConflict, gin.H{"error": "已有一次更新在进行中", "job": job, "bootID": h.bootID})
		return
	}
	h.record(c, "update.apply", "started", "开始一键更新源码并重编译", map[string]interface{}{"job": job.ID, "restart": body.Restart})

	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "state": job.State, "bootID": h.bootID})
}

// Restart answers POST /api/system/update/restart: replace this process so the binary
// already on disk - an update or a rollback that ran without one - becomes the thing
// answering requests. It refuses when there is nothing to activate, because a bounce that
// cannot change anything is a dropped service for no reason.
func (h *UpdateHandler) Restart(c *gin.Context) {
	release, err := update.LockInstall(h.options())
	if err != nil {
		c.JSON(statusForError(err), errorBody(err))
		return
	}
	scheduled := false
	defer func() {
		if !scheduled {
			release()
		}
	}()
	if busy := h.activeJob(); busy != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "更新进行中，等它结束再重启", "job": busy})
		return
	}
	if h.prepareRestart == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "本次启动没有提供重启钩子，请由外部进程管理器重启服务"})
		return
	}
	if needsRestart, _ := h.restartPending(); !needsRestart {
		c.JSON(http.StatusConflict, gin.H{"error": "磁盘上的二进制与当前进程一致，没有待生效的版本", "reason": "nothing_pending"})
		return
	}
	if !update.StampBinary(h.root, h.options()).Exists {
		c.JSON(http.StatusConflict, gin.H{"error": "磁盘上的二进制不存在，无法重启", "reason": "no_binary"})
		return
	}
	activationID := uuid.NewString()
	restart, err := h.prepareRestart(activationID)
	if err != nil {
		failure := &update.Error{Reason: "restart_unavailable", Message: "无法启动磁盘上的二进制：" + err.Error()}
		c.JSON(statusForError(failure), errorBody(failure))
		return
	}
	h.record(c, "update.restart", "started", "重启安装以运行磁盘上的新二进制", nil)
	c.JSON(http.StatusAccepted, gin.H{"restarting": true, "activation_id": activationID, "bootID": h.bootID})
	scheduled = true
	time.AfterFunc(updateRestartDelay, func() { defer release(); restart() })
}

// Job answers GET /api/system/update/job: the running job, or the most recent one, with
// every progress line so far.
func (h *UpdateHandler) Job(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"job": h.latest()})
}

// Rollback answers POST /api/system/update/rollback: back to the commit the last update
// came from, with the binary kept before the swap. It refuses when HEAD has moved since
// that update, rather than resetting away work nobody asked it to touch.
func (h *UpdateHandler) Rollback(c *gin.Context) {
	if busy := h.activeJob(); busy != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "更新进行中，不能同时回滚", "job": busy})
		return
	}
	res, err := update.Rollback(c.Request.Context(), h.options())
	if err != nil {
		h.record(c, "update.rollback", "failure", "回滚失败: "+err.Error(), map[string]interface{}{"error": errMessage(err)})
		c.JSON(statusForError(err), errorBody(err))
		return
	}
	h.record(c, "update.rollback", "success", fmt.Sprintf("已回滚到 %s", res.ToCommit), map[string]interface{}{
		"from": res.FromCommit, "to": res.ToCommit,
	})
	c.JSON(http.StatusOK, gin.H{"result": res})
}

func (h *UpdateHandler) startJob(kind string, restart bool) (*updateJob, bool) {
	h.mu.Lock()
	if h.active != "" {
		return h.jobs[h.active].view(), false
	}
	job := &updateJob{
		ID:      fmt.Sprintf("upd-%d", time.Now().UnixNano()),
		Kind:    kind,
		State:   "running",
		Started: time.Now().Format(time.RFC3339),
		Restart: restart,
	}
	h.jobs[job.ID] = job
	h.order = append(h.order, job.ID)
	for len(h.order) > 20 {
		oldest := h.order[0]
		h.order = h.order[1:]
		if oldest != h.active {
			delete(h.jobs, oldest)
		}
	}
	h.active = job.ID
	snapshot := job.view()
	opts := h.options()
	h.mu.Unlock()

	// The update outlives its HTTP request on purpose: the caller gets a handle, and a
	// browser that closes the tab must not cancel a merge halfway through. Once the
	// working tree has moved there is no going back, so the context is generous with
	// time instead of inheriting the request's deadline.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		onStep := func(s update.Step) {
			h.mu.Lock()
			job.Steps = append(job.Steps, s)
			h.mu.Unlock()
			if h.logger != nil {
				h.logger.Info("更新进度", zap.String("phase", s.Phase), zap.String("message", s.Message))
			}
		}
		var run func(context.Context, update.Options, func(update.Step)) (*update.Result, error) = update.Apply
		if kind == "adopt" {
			run = update.Adopt
		}
		res, err := run(ctx, opts, onStep)
		finish := "succeeded"
		if err != nil {
			finish = "failed"
		}

		h.mu.Lock()
		job.Result = res
		if ue, ok := asUpdateError(err); ok {
			job.Failure = ue
		} else if err != nil {
			job.Failure = &update.Error{Reason: "error", Message: err.Error()}
		}
		job.State = finish
		job.Finished = time.Now().Format(time.RFC3339)
		if job.Restart && finish == "succeeded" && res != nil && res.BinaryBuilt && h.prepareRestart != nil {
			release, lockErr := update.LockInstall(opts)
			if lockErr == nil {
				restart, prepareErr := h.prepareRestart(job.ID)
				if prepareErr == nil {
					h.mu.Unlock()
					// Keep both CLI and HTTP writers out until the old executable is replaced.
					time.AfterFunc(updateRestartDelay, func() {
						defer release()
						defer func() { h.mu.Lock(); h.active = ""; h.mu.Unlock() }()
						restart()
					})
					return
				}
				release()
				job.RestartError = prepareErr.Error()
			} else {
				// Another writer acquired the lock after the build.
				job.RestartError = lockErr.Error()
			}
		}
		// A completed job only reports restartRequested if it actually scheduled one.
		job.Restart = false
		h.active = ""
		h.mu.Unlock()
	}()
	return snapshot, true
}

func (h *UpdateHandler) activeJob() *updateJob {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == "" {
		return nil
	}
	if job := h.jobs[h.active]; job != nil {
		return job.view()
	}
	return nil
}

func (h *UpdateHandler) latest() *updateJob {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.order) == 0 {
		return nil
	}
	if job := h.jobs[h.order[len(h.order)-1]]; job != nil {
		return job.view()
	}
	return nil
}

func (h *UpdateHandler) record(c *gin.Context, action, result, message string, detail map[string]interface{}) {
	if h.audit == nil {
		return
	}
	h.audit.Record(c, audit.Entry{
		Level:        auditLevelFor(result),
		Category:     "update",
		Action:       action,
		Result:       result,
		Message:      message,
		ResourceType: "system",
		ResourceID:   h.root,
		Detail:       detail,
	})
}

func auditLevelFor(result string) string {
	if result == "failure" {
		return "error"
	}
	return "info"
}

func resultOf(checkError string) string {
	if checkError != "" {
		return "failure"
	}
	return "success"
}

func asUpdateError(err error) (*update.Error, bool) {
	if err == nil {
		return nil, false
	}
	if ue, ok := err.(*update.Error); ok {
		return ue, true
	}
	return nil, false
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// statusForError keeps a refusal distinguishable from an accident: the precondition
// failures are the ones the page can act on (409), while anything else is a bad request it
// can show. No case maps a refusal onto a 2xx - a status that reads as accepted would
// contradict the failure the same response carries, and an asynchronous update never asks
// this function (the job's own state says whether it worked).
func statusForError(err error) int {
	ue, ok := asUpdateError(err)
	if !ok {
		return http.StatusInternalServerError
	}
	switch ue.Reason {
	case "local_source_edits", "diverged", "no_state", "moved_since_update", "no_binary", "already_a_repo", "update_busy", "content_path_conflict", "existing_git_metadata":
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func errorBody(err error) gin.H {
	body := gin.H{"error": errMessage(err)}
	if ue, ok := asUpdateError(err); ok {
		body["reason"] = ue.Reason
		if len(ue.Items) > 0 {
			body["items"] = ue.Items
		}
	}
	return body
}
