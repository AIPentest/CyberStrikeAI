package c2

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"cyberstrike-ai/internal/database"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketListener 提供低延迟的双向 WebSocket Beacon。
// 与 HTTP Beacon 相比：
//   - beacon 与服务端保持长连接，无需轮询，新任务可"秒到"；
//   - 适合需要交互式快速响应的场景（如实时键盘 / 流式输出）；
//   - 协议依然走 AES-256-GCM，握手时校验 X-Implant-Token；
//   - 一个 listener 仅处理一个 WS 路径（默认 /ws），但可承载多个并发 implant。
//
// 帧协议（皆为加密后 base64 字符串走 TextMessage）：
//
//	client → server：{"type":"checkin"|"result", "data": <ImplantCheckInRequest|TaskResultReport>}
//	server → client：{"type":"task", "data": <TaskEnvelope>} 或 {"type":"sleep","data":{"sleep":N,"jitter":J}}
type WebSocketListener struct {
	rec     *database.C2Listener
	cfg     *ListenerConfig
	manager *Manager
	logger  *zap.Logger

	srv      *http.Server
	upgrader websocket.Upgrader

	mu      sync.Mutex
	conns   map[string]*wsConn // session_id → 连接
	stopped bool
	stopCh  chan struct{}
}

// wsConn 单个 WS implant 的内存状态
type wsConn struct {
	sessionID string
	ws        *websocket.Conn
	writeMu   sync.Mutex // websocket 同一连接同一时间只能一个 writer
}

// NewWebSocketListener 工厂（注册到 ListenerRegistry["websocket"]）
func NewWebSocketListener(ctx ListenerCreationCtx) (Listener, error) {
	return &WebSocketListener{
		rec:     ctx.Listener,
		cfg:     ctx.Config,
		manager: ctx.Manager,
		logger:  ctx.Logger,
		stopCh:  make(chan struct{}),
		conns:   make(map[string]*wsConn),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// 允许任意 Origin（implant 不带 Origin 或随便填）
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}, nil
}

// Type 类型
func (l *WebSocketListener) Type() string { return string(ListenerTypeWebSocket) }

// Start 启动 HTTP server 接收 WS 升级
func (l *WebSocketListener) Start() error {
	mux := http.NewServeMux()
	wsPath := l.cfg.BeaconCheckInPath
	if wsPath == "" || wsPath == "/check_in" {
		// websocket 默认路径单独定义，避免与 HTTP Beacon 默认路径混淆
		wsPath = "/ws"
	}
	mux.HandleFunc(wsPath, l.handleWS)

	addr := fmt.Sprintf("%s:%d", l.rec.BindHost, l.rec.BindPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			return ErrPortInUse
		}
		return err
	}
	l.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		if err := l.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.logger.Warn("websocket Serve exited", zap.Error(err))
		}
	}()
	go l.taskDispatcherLoop()
	return nil
}

// Stop 优雅关闭：通知所有 WS 客户端，关闭 server
func (l *WebSocketListener) Stop() error {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return nil
	}
	l.stopped = true
	close(l.stopCh)
	conns := make([]*wsConn, 0, len(l.conns))
	for _, c := range l.conns {
		conns = append(conns, c)
	}
	l.conns = make(map[string]*wsConn)
	l.mu.Unlock()
	for _, c := range conns {
		_ = c.ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "shutdown"),
			time.Now().Add(time.Second))
		_ = c.ws.Close()
	}
	if l.srv != nil {
		ctx, cancel := contextWithTimeout(5 * time.Second)
		defer cancel()
		_ = l.srv.Shutdown(ctx)
	}
	return nil
}

func (l *WebSocketListener) handleWS(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get("X-Implant-Token")
	if got == "" || l.rec.ImplantToken == "" ||
		subtle.ConstantTimeCompare([]byte(got), []byte(l.rec.ImplantToken)) != 1 {
		http.NotFound(w, r)
		return
	}
	ws, err := l.upgrader.Upgrade(w, r, nil)
	if err != nil {
		l.logger.Warn("websocket upgrade failed", zap.Error(err))
		return
	}
	go l.handleConn(ws)
}

// handleConn 处理一个 WS 连接的完整生命周期：等待 checkin → 登记 session → 读循环
func (l *WebSocketListener) handleConn(ws *websocket.Conn) {
	ws.SetReadLimit(64 << 20)
	ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 第一帧必须是 checkin
	frameType, body, err := readEncryptedFrame(ws, l.rec.EncryptionKey)
	if err != nil || frameType != "checkin" {
		_ = ws.Close()
		return
	}
	var req ImplantCheckInRequest
	if err := json.Unmarshal(body, &req); err != nil {
		_ = ws.Close()
		return
	}
	if req.SleepSeconds <= 0 {
		req.SleepSeconds = l.cfg.DefaultSleep
	}
	session, err := l.manager.IngestCheckIn(l.rec.ID, req)
	if err != nil {
		_ = ws.Close()
		return
	}
	conn := &wsConn{sessionID: session.ID, ws: ws}
	l.mu.Lock()
	l.conns[session.ID] = conn
	l.mu.Unlock()
	defer func() {
		l.detachConnection(conn)
		_ = ws.Close()
	}()

	// 心跳 goroutine
	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()
	connDone := make(chan struct{})
	defer close(connDone)
	go func() {
		for {
			select {
			case <-l.stopCh:
				return
			case <-connDone:
				return
			case <-pingTicker.C:
				conn.writeMu.Lock()
				_ = ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				conn.writeMu.Unlock()
			}
		}
	}()

	// 主读循环：处理 result 等帧
	for {
		frameType, body, err := readEncryptedFrame(ws, l.rec.EncryptionKey)
		if err != nil {
			return
		}
		switch frameType {
		case "result":
			var report TaskResultReport
			if err := json.Unmarshal(body, &report); err == nil {
				_ = l.manager.IngestTaskResultFromListener(l.rec.ID, conn.sessionID, report)
			}
		case "checkin":
			// 心跳更新：beacon 周期性送上心跳
			var hb ImplantCheckInRequest
			if err := json.Unmarshal(body, &hb); err == nil {
				_ = l.manager.DB().TouchC2Session(session.ID, string(SessionActive), time.Now())
			}
		}
	}
}

// detachConnection only retires the connection that still owns this session.
func (l *WebSocketListener) detachConnection(conn *wsConn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns[conn.sessionID] == conn {
		delete(l.conns, conn.sessionID)
		_ = l.manager.MarkSessionDead(conn.sessionID)
	}
}

// taskDispatcherLoop 周期扫描所有活动 WS 会话，下发任务
func (l *WebSocketListener) taskDispatcherLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-t.C:
			l.mu.Lock()
			snapshot := make([]*wsConn, 0, len(l.conns))
			for _, c := range l.conns {
				snapshot = append(snapshot, c)
			}
			l.mu.Unlock()
			for _, c := range snapshot {
				envelopes, err := l.manager.PopTasksForBeacon(c.sessionID, 20)
				if err != nil || len(envelopes) == 0 {
					continue
				}
				for _, env := range envelopes {
					l.sendTaskFrame(c, env)
				}
			}
		}
	}
}

func (l *WebSocketListener) sendTaskFrame(c *wsConn, env TaskEnvelope) {
	frame := map[string]interface{}{"type": "task", "data": env}
	body, err := json.Marshal(frame)
	if err != nil {
		return
	}
	enc, err := EncryptAESGCM(l.rec.EncryptionKey, body)
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = c.ws.WriteMessage(websocket.TextMessage, []byte(enc))
}

// readEncryptedFrame 读一帧加密 WS 文本，返回类型和明文 data
func readEncryptedFrame(ws *websocket.Conn, key string) (string, []byte, error) {
	mt, raw, err := ws.ReadMessage()
	if err != nil {
		return "", nil, err
	}
	if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
		return "", nil, errors.New("unexpected ws frame type")
	}
	plain, err := DecryptAESGCM(key, string(raw))
	if err != nil {
		return "", nil, err
	}
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(plain, &env); err != nil {
		return "", nil, err
	}
	return env.Type, env.Data, nil
}

// contextWithTimeout 简单封装，避免 listener 文件之间反复 import context
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
