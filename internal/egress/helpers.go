package egress

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
)

// handshaker is satisfied by *tls.Conn and lets dialHTTPConnect complete the
// TLS handshake to an HTTPS proxy under a context deadline.
type handshaker interface {
	HandshakeContext(ctx context.Context) error
}

// tlsClientOnProxy wraps a raw connection to an HTTPS proxy in a TLS client,
// verifying the certificate against the proxy host.
func tlsClientOnProxy(conn net.Conn, serverName string) net.Conn {
	return tls.Client(conn, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
}

// basicAuthEncode returns the base64 of a "user:pass" credential string.
func basicAuthEncode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
