package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// peekedConn 在已预读首字节后仍将连接交给 net/http 或 crypto/tls。
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

// oneConnListener 供 http.Server.Serve 处理单条 TCP 连接（含 keep-alive）。
type oneConnListener struct {
	conn net.Conn
	addr net.Addr
	once sync.Once
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() {
		c = l.conn
		l.conn = nil
	})
	if c == nil {
		return nil, net.ErrClosed
	}
	return c, nil
}

func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return l.addr }

// httpServerForTLSConn 从已有 Server 复制可服务字段，用于已握手 TLS 连接上的 HTTP 服务。
// 不能复制整个 http.Server（内含 atomic/noCopy 字段）。
func httpServerForTLSConn(src *http.Server) *http.Server {
	return &http.Server{
		Handler:                      src.Handler,
		DisableGeneralOptionsHandler: src.DisableGeneralOptionsHandler,
		ReadTimeout:                  src.ReadTimeout,
		ReadHeaderTimeout:            src.ReadHeaderTimeout,
		WriteTimeout:                 src.WriteTimeout,
		IdleTimeout:                  src.IdleTimeout,
		MaxHeaderBytes:               src.MaxHeaderBytes,
		ConnState:                    src.ConnState,
		ErrorLog:                     src.ErrorLog,
		BaseContext:                  src.BaseContext,
		ConnContext:                  src.ConnContext,
	}
}

func isTLSHandshakeRecord(b byte) bool {
	return b == 0x16
}

func newHTTPToHTTPSRedirectHandler(httpsPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		var target string
		if httpsPort == 443 {
			target = fmt.Sprintf("https://%s%s", host, r.URL.RequestURI())
		} else {
			target = fmt.Sprintf("https://%s:%d%s", host, httpsPort, r.URL.RequestURI())
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

func portFromListenAddr(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 443
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return 443
	}
	return p
}

func ensureMainTLSConfigCerts(mode mainTLSMode, tlsConf *tls.Config, certFile, keyFile string) (*tls.Config, error) {
	if mode != mainTLSFromFiles {
		return tlsConf, nil
	}
	if tlsConf == nil {
		tlsConf = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if len(tlsConf.Certificates) > 0 {
		return tlsConf, nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	tlsConf.Certificates = []tls.Certificate{cert}
	return tlsConf, nil
}

type mainServerMux struct {
	ln          net.Listener
	httpsSrv    *http.Server
	redirectSrv *http.Server
	logger      *zap.Logger
}

func newMainServerMux(ln net.Listener, httpsSrv *http.Server, httpsPort int, logger *zap.Logger) *mainServerMux {
	return &mainServerMux{
		ln:          ln,
		httpsSrv:    httpsSrv,
		redirectSrv: &http.Server{Handler: newHTTPToHTTPSRedirectHandler(httpsPort), ReadHeaderTimeout: 10 * time.Second},
		logger:      logger,
	}
}

func (m *mainServerMux) Serve() error {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return http.ErrServerClosed
			}
			return err
		}
		go m.handleConn(conn)
	}
}

func (m *mainServerMux) handleConn(raw net.Conn) {
	if err := raw.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = raw.Close()
		return
	}
	br := bufio.NewReader(raw)
	b, err := br.Peek(1)
	if err != nil {
		_ = raw.Close()
		return
	}
	_ = raw.SetReadDeadline(time.Time{})

	pc := &peekedConn{Conn: raw, r: br}
	ocl := &oneConnListener{conn: pc, addr: raw.LocalAddr()}

	if isTLSHandshakeRecord(b[0]) {
		m.serveHTTPS(pc, raw.LocalAddr())
		return
	}
	if err := m.redirectSrv.Serve(ocl); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		m.logger.Debug("HTTP redirect connection handling finished", zap.Error(err))
	}
}

// serveHTTPS 在已嗅探为 TLS 的连接上完成握手，再按 ALPN 走 HTTP/2 或 HTTP/1.1。
// 不能对同一 http.Server 并发调用 Serve(TLSConfig!=nil)，否则握手/ALPN 会异常（浏览器 ERR_SSL_PROTOCOL_ERROR）。
func (m *mainServerMux) serveHTTPS(pc *peekedConn, localAddr net.Addr) {
	tlsConn := tls.Server(pc, m.httpsSrv.TLSConfig)
	handCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(handCtx); err != nil {
		m.logger.Debug("TLS handshake failed", zap.Error(err))
		_ = pc.Close()
		return
	}

	srv := m.httpsSrv
	if srv.TLSNextProto != nil {
		proto := tlsConn.ConnectionState().NegotiatedProtocol
		if fn := srv.TLSNextProto[proto]; fn != nil {
			fn(srv, tlsConn, srv.Handler)
			return
		}
	}

	plain := httpServerForTLSConn(srv)
	ocl := &oneConnListener{conn: tlsConn, addr: localAddr}
	if err := plain.Serve(ocl); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		m.logger.Debug("HTTPS connection handling finished", zap.Error(err))
	}
}

func (m *mainServerMux) Shutdown(ctx context.Context) error {
	_ = m.ln.Close()
	var err1, err2 error
	if m.httpsSrv != nil {
		err1 = m.httpsSrv.Shutdown(ctx)
	}
	if m.redirectSrv != nil {
		err2 = m.redirectSrv.Shutdown(ctx)
	}
	if err1 != nil {
		return err1
	}
	return err2
}
