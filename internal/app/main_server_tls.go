package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

// mainTLSMode 主 Web 服务 TLS 启动方式。
type mainTLSMode int

const (
	mainTLSOff mainTLSMode = iota
	mainTLSFromFiles
	mainTLSAutoSelfSigned
)

// prepareMainServerTLS 根据 server 配置决定主站是否启用 HTTPS（及 HTTP/2 协商）。
// fromFiles：使用 tls_cert_path + tls_key_path，由 http.Server.ListenAndServeTLS 加载 PEM。
// autoSelfSigned：tls_auto_self_sign 生成并复用的自签证书，仅用于本地/测试。
func prepareMainServerTLS(cfg *config.ServerConfig, configDir string) (mode mainTLSMode, tlsConf *tls.Config, certFile, keyFile string, err error) {
	if cfg == nil || !config.MainWebUIUsesHTTPS(cfg) {
		return mainTLSOff, nil, "", "", nil
	}
	certFile = strings.TrimSpace(cfg.TLSCertPath)
	keyFile = strings.TrimSpace(cfg.TLSKeyPath)
	if certFile != "" && keyFile != "" {
		// 证书由 ListenAndServeTLS 从文件加载；此处仅提供最小 TLS 配置供 http2.ConfigureServer 合并 ALPN。
		return mainTLSFromFiles, &tls.Config{MinVersion: tls.VersionTLS12}, certFile, keyFile, nil
	}
	if cfg.TLSAutoSelfSign {
		path := filepath.Join(configDir, "data", "tls", "self-signed.pem")
		cert, certErr := loadOrCreateMainServerSelfSignedCert(path)
		if certErr != nil {
			return mainTLSOff, nil, "", "", fmt.Errorf("读取或保存自签 TLS 证书 %s: %w", path, certErr)
		}
		tlsConf = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
		return mainTLSAutoSelfSigned, tlsConf, "", "", nil
	}
	return mainTLSOff, nil, "", "", fmt.Errorf("server: 已启用 TLS（tls_enabled / tls_auto_self_sign / 证书路径），请设置 tls_cert_path 与 tls_key_path，或将 tls_auto_self_sign 设为 true（仅测试环境）")
}

func loadOrCreateMainServerSelfSignedCert(path string) (tls.Certificate, error) {
	bundle, err := os.ReadFile(path)
	if err == nil {
		return tls.X509KeyPair(bundle, bundle)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return tls.Certificate{}, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return tls.Certificate{}, err
	}
	bundle, err = generateMainServerSelfSignedPEM()
	if err != nil {
		return tls.Certificate{}, err
	}
	f, err := os.CreateTemp(dir, ".self-signed-*")
	if err != nil {
		return tls.Certificate{}, err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(bundle)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return tls.Certificate{}, err
	}
	// Publish the complete pair once, even if two instances start concurrently.
	if err := os.Link(f.Name(), path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return tls.Certificate{}, err
		}
		bundle, err = os.ReadFile(path)
		if err != nil {
			return tls.Certificate{}, err
		}
	}
	return tls.X509KeyPair(bundle, bundle)
}

func generateMainServerSelfSignedPEM() ([]byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "CyberStrikeAI"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return append(certPEM, keyPEM...), nil
}
