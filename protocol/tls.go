//go:build !notls

package protocol

import (
	"crypto/tls"
	"fmt"
	"net"

	utls "github.com/refraction-networking/utls"
)

// NewTLSConfig returns a *tls.Config suitable for the transport layer.
// Client mode: uses Chrome-compatible parameters for uTLS-based dialers.
// Server mode: returns minimal config (certificate loading must be done by caller).
//
// The returned *tls.Config uses Chrome-like cipher suites and curves so that
// uTLS-based dialers (see UTLSDial) can negotiate a fingerprint-consistent
// handshake. When the build tag "notls" is set, falls back to crypto/tls securely.
func NewTLSConfig(serverName string, isClient bool) *tls.Config {
	cfg := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS13,
	}
	return cfg
}

// UTLSDial dials a TCP connection and performs a uTLS handshake with
// Chrome fingerprint mimicry. Returns a net.Conn that can be used for
// WebSocket upgrade or direct TLS tunneling.
func UTLSDial(server string, port int, sni string) (net.Conn, error) {
	tcpConn, err := net.Dial("tcp", net.JoinHostPort(server, fmt.Sprintf("%d", port)))
	if err != nil {
		return nil, fmt.Errorf("TCP dial: %w", err)
	}

	// HTTP/1.1 only (NOT h2) — gorilla/websocket requires HTTP/1.1 for Upgrade.
	// Cloudflare prefers h2, which breaks WebSocket upgrade.
	// HelloChrome_Auto advertises h2 in ALPN. We use HelloCustom to override.
	uconn := utls.UClient(tcpConn, &utls.Config{
		ServerName:         sni,
		InsecureSkipVerify: false,
		MinVersion:         utls.VersionTLS13,
	}, utls.HelloCustom)

	// Build a Chrome 120-style ClientHello with only http/1.1 in ALPN.
	spec := &utls.ClientHelloSpec{
		TLSVersMax: utls.VersionTLS13,
		TLSVersMin: utls.VersionTLS12,
		CipherSuites: []uint16{
			utls.TLS_AES_128_GCM_SHA256,
			utls.TLS_AES_256_GCM_SHA384,
			utls.TLS_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			utls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
		CompressionMethods: []uint8{0},
		Extensions: []utls.TLSExtension{
			&utls.SNIExtension{ServerName: sni},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{Renegotiation: utls.RenegotiateOnceAsClient},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{
				utls.X25519, utls.CurveP256, utls.CurveP384,
			}},
			&utls.SupportedPointsExtension{SupportedPoints: []byte{0}},
			&utls.SessionTicketExtension{},
			&utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&utls.StatusRequestExtension{},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256,
				utls.PSSWithSHA256,
				utls.PKCS1WithSHA256,
				utls.ECDSAWithP384AndSHA384,
				utls.PSSWithSHA384,
				utls.PKCS1WithSHA384,
				utls.PSSWithSHA512,
				utls.PKCS1WithSHA512,
			}},
			&utls.SCTExtension{},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{
				{Group: utls.X25519},
			}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SupportedVersionsExtension{Versions: []uint16{
				utls.VersionTLS13,
				utls.VersionTLS12,
			}},
			&utls.UtlsExtendedMasterSecretExtension{},
		},
	}

	if err := uconn.ApplyPreset(spec); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("uTLS custom spec: %w", err)
	}

	if err := uconn.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("uTLS handshake: %w", err)
	}

	// Verify server negotiated http/1.1
	if uconn.ConnectionState().NegotiatedProtocol != "http/1.1" {
		tcpConn.Close()
		return nil, fmt.Errorf("server negotiated %s, need http/1.1", uconn.ConnectionState().NegotiatedProtocol)
	}

	return uconn, nil
}

// NewUTLSConn wraps an existing net.Conn with uTLS using Chrome fingerprint.
// Useful when you need to upgrade a raw connection (e.g., after SOCKS5 proxy).
func NewUTLSConn(raw net.Conn, sni string) (net.Conn, error) {
	uconn := utls.UClient(raw, &utls.Config{
		ServerName:         sni,
		InsecureSkipVerify: false,
		MinVersion:         utls.VersionTLS13,
	}, utls.HelloChrome_Auto)
	if err := uconn.Handshake(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("uTLS handshake: %w", err)
	}
	return uconn, nil
}
