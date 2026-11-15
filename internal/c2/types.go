// Package c2 实现 CyberStrikeAI 内置 C2（Command & Control）框架。
//
// 设计概述：
//   - Manager 作为统一入口，被 internal/app 实例化并注入到所有需要操控 C2 的组件
//     （HTTP handler、MCP 工具、HITL 桥、攻击链记录器等）。
//   - Listener 是抽象接口，下挂 tcp_reverse / http_beacon / https_beacon / websocket
//     等不同传输方式的具体实现，全部通过 listener.Registry 工厂创建。
//   - 任务调度走数据库（c2_tasks 表）+ 内存事件总线（EventBus）混合：
//   - 状态变化与历史记录靠 SQLite 实现持久化与重启恢复；
//   - 高频实时通知（如新任务结果）通过 EventBus 推送给 SSE/WS 订阅者，避免轮询。
//   - Crypto 层固定 AES-256-GCM，每个 Listener 独立 32 字节密钥；密钥仅服务端持有
//     和编译期注入到 implant，事件流不允许导出明文密钥。
package c2

import (
	"errors"
	"strings"
	"time"
)

// ListenerType 监听器类型，与 c2_listeners.type 字段一致
type ListenerType string

const (
	ListenerTypeTCPReverse  ListenerType = "tcp_reverse"
	ListenerTypeHTTPBeacon  ListenerType = "http_beacon"
	ListenerTypeHTTPSBeacon ListenerType = "https_beacon"
	ListenerTypeWebSocket   ListenerType = "websocket"
)

// AllListenerTypes 列出所有受支持的监听器类型，便于校验与前端枚举
func AllListenerTypes() []ListenerType {
	return []ListenerType{
		ListenerTypeTCPReverse,
		ListenerTypeHTTPBeacon,
		ListenerTypeHTTPSBeacon,
		ListenerTypeWebSocket,
	}
}

// IsValidListenerType 校验前端/MCP 入参是否为合法 type
func IsValidListenerType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, lt := range AllListenerTypes() {
		if string(lt) == t {
			return true
		}
	}
	return false
}

// SessionStatus 与 c2_sessions.status 一致
type SessionStatus string

const (
	SessionActive   SessionStatus = "active"
	SessionSleeping SessionStatus = "sleeping"
	SessionDead     SessionStatus = "dead"
	SessionKilled   SessionStatus = "killed"
)

// TaskStatus 与 c2_tasks.status 一致
type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskSent      TaskStatus = "sent"
	TaskRunning   TaskStatus = "running"
	TaskSuccess   TaskStatus = "success"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// TaskType 任务类型（与 beacon 端协商，避免硬编码字符串）
type TaskType string

const (
	// 通用任务
	TaskTypeExec       TaskType = "exec"        // 执行任意命令（shell -c）
	TaskTypeShell      TaskType = "shell"       // 交互式命令（保持 cwd）
	TaskTypePwd        TaskType = "pwd"         // 当前目录
	TaskTypeCd         TaskType = "cd"          // 切目录
	TaskTypeLs         TaskType = "ls"          // 列目录
	TaskTypePs         TaskType = "ps"          // 列进程
	TaskTypeKillProc   TaskType = "kill_proc"   // 杀进程
	TaskTypeUpload     TaskType = "upload"      // 推文件到目标
	TaskTypeDownload   TaskType = "download"    // 拉文件回本机
	TaskTypeScreenshot TaskType = "screenshot"  // 截图
	TaskTypeSleep      TaskType = "sleep"       // 调整心跳节律
	TaskTypeExit       TaskType = "exit"        // 让 implant 退出（不会自删二进制）
	TaskTypeSelfDelete TaskType = "self_delete" // 退出 + 自删二进制（持久化清理）
	// 高级任务
	TaskTypePortFwd      TaskType = "port_fwd"
	TaskTypeSocksStart   TaskType = "socks_start"
	TaskTypeSocksStop    TaskType = "socks_stop"
	TaskTypeLoadAssembly TaskType = "load_assembly"
	TaskTypePersist      TaskType = "persist"
)

// AllTaskTypes 全部 task_type，便于工具 schema 列出 enum
func AllTaskTypes() []TaskType {
	return []TaskType{
		TaskTypeExec, TaskTypeShell,
		TaskTypePwd, TaskTypeCd, TaskTypeLs, TaskTypePs, TaskTypeKillProc,
		TaskTypeUpload, TaskTypeDownload, TaskTypeScreenshot,
		TaskTypeSleep, TaskTypeExit, TaskTypeSelfDelete,
		TaskTypePortFwd, TaskTypeSocksStart, TaskTypeSocksStop, TaskTypeLoadAssembly,
		TaskTypePersist,
	}
}

// IsDangerousTaskType 标记需要 HITL 二次确认的任务类型；
// 与 internal/handler/hitl.go 现有的 tool_whitelist 概念呼应：白名单外 → 走审批。
func IsDangerousTaskType(t TaskType) bool {
	switch t {
	case TaskTypeKillProc, TaskTypeUpload, TaskTypeSelfDelete,
		TaskTypePortFwd, TaskTypeSocksStart, TaskTypeLoadAssembly, TaskTypePersist:
		return true
	}
	return false
}

// ListenerConfig 解码后的监听器运行配置（来自 c2_listeners.config_json）
type ListenerConfig struct {
	// HTTP/HTTPS Beacon 公共字段
	BeaconCheckInPath string `json:"beacon_check_in_path,omitempty"` // 默认 "/check_in"
	BeaconTasksPath   string `json:"beacon_tasks_path,omitempty"`    // 默认 "/tasks"
	BeaconResultPath  string `json:"beacon_result_path,omitempty"`   // 默认 "/result"
	BeaconUploadPath  string `json:"beacon_upload_path,omitempty"`   // 默认 "/upload"
	BeaconFilePath    string `json:"beacon_file_path,omitempty"`     // 默认 "/file/"
	// HTTPS 专属
	TLSCertPath     string `json:"tls_cert_path,omitempty"`
	TLSKeyPath      string `json:"tls_key_path,omitempty"`
	TLSAutoSelfSign bool   `json:"tls_auto_self_sign,omitempty"` // true：找不到证书时自动生成自签
	// 客户端默认参数（写到 c2_sessions 初值，beacon 也可在 check-in 时覆写）
	DefaultSleep  int `json:"default_sleep,omitempty"`  // 秒，默认 5
	DefaultJitter int `json:"default_jitter,omitempty"` // 0-100，默认 0
	// OPSEC：可选命令黑名单（正则）
	CommandDenyRegex []string `json:"command_deny_regex,omitempty"`
	// 任务并发上限（每个会话同时下发的最大任务数，0 表示不限制）
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`
	// CallbackHost 植入端/Payload 使用的回连主机名（可选）；与 bind_host 分离，便于 NAT/ECS 等场景
	CallbackHost string `json:"callback_host,omitempty"`
	// AllowLegacyShell 为 true 时 tcp_reverse 允许未加密的经典 bash/nc 反弹 shell 登记会话（默认 false，公网部署强烈不建议开启）
	AllowLegacyShell bool `json:"allow_legacy_shell,omitempty"`
}

// ApplyDefaults 对未填字段填默认值；调用方负责持久化时序列化新值
func (c *ListenerConfig) ApplyDefaults() {
	if strings.TrimSpace(c.BeaconCheckInPath) == "" {
		c.BeaconCheckInPath = "/check_in"
	}
	if strings.TrimSpace(c.BeaconTasksPath) == "" {
		c.BeaconTasksPath = "/tasks"
	}
	if strings.TrimSpace(c.BeaconResultPath) == "" {
		c.BeaconResultPath = "/result"
	}
	if strings.TrimSpace(c.BeaconUploadPath) == "" {
		c.BeaconUploadPath = "/upload"
	}
	if strings.TrimSpace(c.BeaconFilePath) == "" {
		c.BeaconFilePath = "/file/"
	}
	if c.DefaultSleep <= 0 {
		c.DefaultSleep = 5
	}
	if c.DefaultJitter < 0 {
		c.DefaultJitter = 0
	}
	if c.DefaultJitter > 100 {
		c.DefaultJitter = 100
	}
}

// ImplantCheckInRequest beacon → 服务端的注册/心跳请求体（已解密后的明文）
type ImplantCheckInRequest struct {
	ImplantUUID   string                 `json:"uuid"`
	Hostname      string                 `json:"hostname"`
	Username      string                 `json:"username"`
	OS            string                 `json:"os"`
	Arch          string                 `json:"arch"`
	PID           int                    `json:"pid"`
	ProcessName   string                 `json:"process_name"`
	IsAdmin       bool                   `json:"is_admin"`
	InternalIP    string                 `json:"internal_ip"`
	UserAgent     string                 `json:"user_agent,omitempty"`
	SleepSeconds  int                    `json:"sleep_seconds"`
	JitterPercent int                    `json:"jitter_percent"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ImplantCheckInResponse 服务端回执
type ImplantCheckInResponse struct {
	SessionID  string `json:"session_id"`
	NextSleep  int    `json:"next_sleep"`
	NextJitter int    `json:"next_jitter"`
	HasTasks   bool   `json:"has_tasks"`
	ServerTime int64  `json:"server_time"`
}

// TaskEnvelope 服务端 → beacon 的任务派发载体
type TaskEnvelope struct {
	TaskID   string                 `json:"task_id"`
	TaskType string                 `json:"task_type"`
	Payload  map[string]interface{} `json:"payload"`
}

// TaskResultReport beacon → 服务端的任务结果回传
type TaskResultReport struct {
	TaskID     string `json:"task_id"`
	Success    bool   `json:"success"`
	Output     string `json:"output,omitempty"`
	OutputB64  string `json:"output_b64,omitempty"` // 原始控制台字节（base64），避免 JSON 破坏非 UTF-8 输出
	Error      string `json:"error,omitempty"`
	ErrorB64   string `json:"error_b64,omitempty"`
	BlobBase64 string `json:"blob_b64,omitempty"`    // 如截图二进制
	BlobSuffix string `json:"blob_suffix,omitempty"` // 如 ".png"
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at"`
}

// CommonError C2 模块统一错误类型，便于 handler 层映射 HTTP 状态码
type CommonError struct {
	Code    string
	Message string
	HTTP    int
}

func (e *CommonError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Sentinel errors，便于 errors.Is 比较
var (
	ErrListenerNotFound = &CommonError{Code: "listener_not_found", Message: "监听器不存在", HTTP: 404}
	ErrSessionNotFound  = &CommonError{Code: "session_not_found", Message: "会话不存在", HTTP: 404}
	ErrTaskNotFound     = &CommonError{Code: "task_not_found", Message: "任务不存在", HTTP: 404}
	ErrProfileNotFound  = &CommonError{Code: "profile_not_found", Message: "Profile 不存在", HTTP: 404}
	ErrInvalidInput     = &CommonError{Code: "invalid_input", Message: "参数非法", HTTP: 400}
	ErrAuthFailed       = &CommonError{Code: "auth_failed", Message: "鉴权失败", HTTP: 401}
	ErrPortInUse        = &CommonError{Code: "port_in_use", Message: "端口已被占用", HTTP: 409}
	ErrListenerRunning  = &CommonError{Code: "listener_running", Message: "监听器已在运行", HTTP: 409}
	ErrListenerStopped  = &CommonError{Code: "listener_stopped", Message: "监听器未运行", HTTP: 409}
	ErrUnsupportedType  = &CommonError{Code: "unsupported_type", Message: "不支持的监听器类型", HTTP: 400}
)

// SafeBindPort 校验端口范围
func SafeBindPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be in 1..65535")
	}
	return nil
}

// NowUnixMillis 统一时间戳工具
func NowUnixMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
