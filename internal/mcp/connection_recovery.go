package mcp

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	// externalReconnectMinInterval 两次自动重连之间的最短间隔
	externalReconnectMinInterval = 30 * time.Second
	// externalReconnectMaxBackoff 指数退避上限
	externalReconnectMaxBackoff = 5 * time.Minute
)

// isConnectionDeadError 判断错误是否表示底层传输已断开（而非调用方主动取消或超时）。
func isConnectionDeadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "client is closing") ||
		strings.Contains(s, "connection closed") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe")
}

// handleConnectionDead 在 ListTools/CallTool 等操作失败且判定为断连时，标记客户端并调度重连。
func (m *ExternalMCPManager) handleConnectionDead(name string, client ExternalMCPClient, err error) {
	if !isConnectionDeadError(err) {
		return
	}
	m.logger.Warn("Detected that the external MCP connection dropped; automatic reconnection will be attempted",
		zap.String("name", name),
		zap.Error(err),
	)
	m.markClientDisconnected(name, client, err)
	m.scheduleReconnect(name)
}

func (m *ExternalMCPManager) markClientDisconnected(name string, client ExternalMCPClient, err error) {
	if lazy, ok := client.(*lazySDKClient); ok {
		lazy.markDisconnected()
	}
	m.mu.Lock()
	if err != nil {
		m.errors[name] = "connection dropped: " + err.Error()
	}
	m.mu.Unlock()
	m.toolCountsMu.Lock()
	m.toolCounts[name] = 0
	m.toolCountsMu.Unlock()
}

func (m *ExternalMCPManager) onClientConnected(name string) {
	m.clearReconnectState(name)
}

func (m *ExternalMCPManager) clearReconnectState(name string) {
	m.reconnectMu.Lock()
	delete(m.reconnectAttempts, name)
	delete(m.reconnectLastTry, name)
	delete(m.reconnecting, name)
	m.reconnectMu.Unlock()
}

func (m *ExternalMCPManager) reconnectBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	d := externalReconnectMinInterval
	for i := 1; i < attempts && d < externalReconnectMaxBackoff; i++ {
		d *= 2
	}
	if d > externalReconnectMaxBackoff {
		return externalReconnectMaxBackoff
	}
	return d
}

func (m *ExternalMCPManager) scheduleReconnect(name string) {
	m.mu.RLock()
	cfg, exists := m.configs[name]
	enabled := exists && m.isEnabled(cfg)
	m.mu.RUnlock()
	if !enabled {
		return
	}
	go m.tryReconnect(name)
}

func (m *ExternalMCPManager) tryReconnect(name string) {
	m.reconnectMu.Lock()
	if m.reconnecting[name] {
		m.reconnectMu.Unlock()
		return
	}
	attempts := m.reconnectAttempts[name]
	if wait := m.reconnectBackoff(attempts); wait > 0 {
		if last, ok := m.reconnectLastTry[name]; ok {
			if elapsed := time.Since(last); elapsed < wait {
				remaining := wait - elapsed
				m.reconnectMu.Unlock()
				m.scheduleReconnectAfter(name, remaining)
				return
			}
		}
	}
	m.reconnecting[name] = true
	m.reconnectMu.Unlock()

	defer func() {
		m.reconnectMu.Lock()
		delete(m.reconnecting, name)
		m.reconnectMu.Unlock()
	}()

	m.mu.RLock()
	cfg, exists := m.configs[name]
	enabled := exists && m.isEnabled(cfg)
	client, hasClient := m.clients[name]
	connecting := hasClient && client.GetStatus() == "connecting"
	m.mu.RUnlock()

	if !enabled {
		m.logger.Debug("Skipping automatic reconnection (the external MCP is disabled)", zap.String("name", name))
		return
	}
	if connecting {
		m.logger.Debug("Skipping automatic reconnection (a connection attempt is in progress)", zap.String("name", name))
		return
	}

	m.reconnectMu.Lock()
	m.reconnectLastTry[name] = time.Now()
	m.reconnectAttempts[name] = attempts + 1
	attemptNum := m.reconnectAttempts[name]
	m.reconnectMu.Unlock()

	m.logger.Info("Automatically reconnecting the external MCP",
		zap.String("name", name),
		zap.Int("attempt", attemptNum),
	)

	if err := m.startClient(name, true); err != nil {
		m.logger.Warn("Automatic reconnection of the external MCP failed",
			zap.String("name", name),
			zap.Error(err),
		)
	}
}

// scheduleReconnectAfterFailure 在自动重连失败后，按当前退避间隔预约下一次重试。
func (m *ExternalMCPManager) scheduleReconnectAfterFailure(name string) {
	m.mu.RLock()
	cfg, exists := m.configs[name]
	enabled := exists && m.isEnabled(cfg)
	m.mu.RUnlock()
	if !enabled {
		return
	}
	m.reconnectMu.Lock()
	wait := m.reconnectBackoff(m.reconnectAttempts[name])
	m.reconnectMu.Unlock()
	m.logger.Info("Automatic reconnection failed; another attempt will follow after the backoff interval",
		zap.String("name", name),
		zap.Duration("after", wait),
	)
	m.scheduleReconnectAfter(name, wait)
}

// scheduleReconnectAfter 在 delay 后触发 tryReconnect（delay<=0 时立即执行）。
func (m *ExternalMCPManager) scheduleReconnectAfter(name string, delay time.Duration) {
	if delay <= 0 {
		go m.tryReconnect(name)
		return
	}
	time.AfterFunc(delay, func() {
		m.tryReconnect(name)
	})
}
