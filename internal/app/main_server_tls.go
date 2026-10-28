package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

// mainTLSMode 主 Web 服务 TLS 启动方式。
type mainTLSMode int

const (
	mainTLSOff mainTLSMode = iota
	mainTLSFromFiles
	mainTLSInMemorySelfSigned
)

// prepareMainServerTLS 根据 server 配置决定主站是否启用 HTTPS（及 HTTP/2 协商）。
// fromFiles：使用 tls_cert_path + tls_key_path，由 http.Server.ListenAndServeTLS 加载 PEM。
// inMemory：tls_auto_self_sign 生成的自签证书，仅用于本地/测试。
func prepareMainServerTLS(cfg *config.ServerConfig) (mode mainTLSMode, tlsConf *tls.Config, certFile, keyFile string, err error) {
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
		cert, genErr := generateMainServerSelfSignedCert()
		if genErr != nil {
			return mainTLSOff, nil, "", "", fmt.Errorf("failed to generate a self-signed TLS certificate: %w", genErr)
		}
		tlsConf = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
		return mainTLSInMemorySelfSigned, tlsConf, "", "", nil
	}
	return mainTLSOff, nil, "", "", fmt.Errorf("server: TLS is enabled (tls_enabled / tls_auto_self_sign / certificate paths); set tls_cert_path and tls_key_path, or set tls_auto_self_sign to true (tests only)")
}

func generateMainServerSelfSignedCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, err
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
