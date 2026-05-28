// Package core — VPN-движок Hasta-Vaquet.
// Прослойка совместимости. Новый код использует protocol/ напрямую.
package core

import "hasta-vaquet/protocol"

// Типы и функции, экспортируемые из protocol/ для обратной совместимости.
type CipherPack = protocol.CipherPack
type User = protocol.User

var (
	NewCipherPack     = protocol.NewCipherPack
	NewNoopCipherPack = protocol.NewNoopCipherPack
	Fnv1a16           = protocol.Fnv1a16
	Fnv1a64           = protocol.Fnv1a64
	DeriveKey         = protocol.DeriveKey
)

// Deprecated: используйте protocol.CipherSuite.
const (
	CipherAES256GCM        = protocol.CipherAES256GCM
	CipherChaCha20Poly1305 = protocol.CipherChaCha20Poly1305
)
