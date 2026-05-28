//go:build notls

package protocol

import (
	"crypto/tls"
	"fmt"
	"net"
)

// NewTLSConfig returns a *tls.Config using standard crypto/tls with secure defaults.
// Used when the build tag "notls" is set (uTLS not compiled in).
func NewTLSConfig(serverName string, isClient bool) *tls.Config {
	return &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
	}
}

// UTLSDial is not available in fallback mode.
// Returns an error indicating uTLS is not compiled in.
func UTLSDial(server string, port int, sni string) (net.Conn, error) {
	return nil, fmt.Errorf("uTLS not available: build with !notls tag")
}

// NewUTLSConn is not available in fallback mode.
func NewUTLSConn(raw net.Conn, sni string) (net.Conn, error) {
	return nil, fmt.Errorf("uTLS not available: build with !notls tag")
}
