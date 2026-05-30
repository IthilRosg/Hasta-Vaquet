// Package protocol — криптографический слой Hasta-Vaquet.
// cipher.go: шифрование, HMAC, nonce, padding.
package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
)

// Cipher modes for packet format selection.
const (
	CipherModeStandard = iota // current format: HMAC(4) + DynamicID(2) + Nonce(12) + AES-GCM
	CipherModeQUIC            // QUIC Short Header v1 format
)

// Buffer pools to reduce allocations in Encrypt/Decrypt.
var bufPool = sync.Pool{
	New: func() any { return make([]byte, 65535) },
}

// padBufPool предоставляет буферы со случайными байтами для padding'а.
// Буферы одноразовые: берём, копируем сколько нужно, возвращаем.
var padBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64)
		rand.Read(b)
		return b
	},
}

// fastPRNG — xorshift64* для генерации длины padding'а.
// НЕ thread-safe — каждый CipherPack владеет своим экземпляром.
type fastPRNG struct {
	state uint64
}

func (r *fastPRNG) Uint32() uint32 {
	r.state ^= r.state << 13
	r.state ^= r.state >> 17
	r.state ^= r.state << 5
	return uint32(r.state)
}

func Fnv1a16(data []byte) uint16 {
	h := uint64(14695981039346656037)
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return uint16(h & 0xFFFF)
}

func Fnv1a64(data []byte, seed uint64) uint64 {
	hash := seed ^ 14695981039346656037
	for _, b := range data {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return hash
}

// CipherPack — криптографический контекст для Encrypt/Decrypt.
// Каждый экземпляр используется строго из одной горутины — мутекс не нужен.
// Для разных горутин создавайте отдельные CipherPack из одного ключа.
type CipherPack struct {
	key       [32]byte
	gcm       cipher.AEAD   // AEAD не thread-safe, но каждый CP однопоточный
	nonce     atomic.Uint64 // monotonic counter (standard mode)
	quicNonce atomic.Uint64 // monotonic counter for QUIC mode
	prng      fastPRNG      // быстрый PRNG для padding (без блокировок)
	noEncrypt bool          // true = Encrypt/Decrypt без крипты (для замера накладных расходов)
	Mode      int           // CipherModeStandard или CipherModeQUIC
}

func NewCipherPack(secretKey []byte) (*CipherPack, error) {
	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var k [32]byte
	copy(k[:], secretKey)

	cp := &CipherPack{
		key: k,
		gcm: gcm,
	}

	// Seed PRNG из crypto/rand (8 байт → xorshift64)
	var seedBuf [8]byte
	rand.Read(seedBuf[:])
	cp.prng.state = binary.LittleEndian.Uint64(seedBuf[:])

	// Nonce стартует со случайным смещением
	cp.nonce.Store(uint64(cp.prng.Uint32()))
	return cp, nil
}

func NewNoopCipherPack() *CipherPack {
	return &CipherPack{noEncrypt: true}
}

// quicNonceFromPacket derives a 12-byte AES-GCM nonce deterministically from
// the shared key and packet number. Both encrypt and decrypt sides compute
// the same nonce without transmitting it.
func quicNonceFromPacket(key [32]byte, packetNum uint32) [12]byte {
	var buf [2 + 4]byte
	buf[0] = byte(len(key))
	buf[1] = byte(packetNum >> 24)
	binary.BigEndian.PutUint32(buf[2:], packetNum)
	h := sha256.Sum256(append(key[:], buf[:]...))
	var nonce [12]byte
	copy(nonce[:], h[:12])
	return nonce
}

func (cp *CipherPack) Encrypt(plaintext []byte, shortID uint16, routingSalt string) ([]byte, error) {
	if cp.noEncrypt {
		buf := make([]byte, 2+len(plaintext))
		binary.BigEndian.PutUint16(buf[:2], uint16(len(plaintext)))
		copy(buf[2:], plaintext)
		return buf, nil
	}

	if cp.Mode == CipherModeQUIC {
		return cp.encryptQUIC(plaintext, shortID, routingSalt)
	}

	n := cp.nonce.Add(1)
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[:8], n)
	binary.BigEndian.PutUint32(nonce[8:], cp.prng.Uint32())

	padLen := int(cp.prng.Uint32() % 41)

	// Reuse буфер для inner (plaintext + padding)
	innerLen := 2 + len(plaintext) + padLen
	inner := bufPool.Get().([]byte)[:innerLen]
	defer bufPool.Put(inner[:cap(inner)])

	binary.BigEndian.PutUint16(inner[:2], uint16(len(plaintext)))
	copy(inner[2:], plaintext)
	if padLen > 0 {
		padBuf := padBufPool.Get().([]byte)
		copy(inner[2+len(plaintext):], padBuf[:padLen])
		padBufPool.Put(padBuf)
	}

	routeMask := Fnv1a16(append([]byte(routingSalt), nonce[:]...))
	dynamicID := shortID ^ routeMask

	var authData [14]byte
	binary.BigEndian.PutUint16(authData[:2], dynamicID)
	copy(authData[2:], nonce[:])

	mac := hmac.New(sha256.New, cp.key[:])
	mac.Write(authData[:])
	marker := mac.Sum(nil)[:4]
	marker[0] |= 0x40

	ciphertext := cp.gcm.Seal(nil, nonce[:], inner, nil)

	buf := make([]byte, 4+2+12+len(ciphertext))
	copy(buf[:4], marker)
	binary.BigEndian.PutUint16(buf[4:6], dynamicID)
	copy(buf[6:18], nonce[:])
	copy(buf[18:], ciphertext)
	return buf, nil
}

func (cp *CipherPack) encryptQUIC(plaintext []byte, shortID uint16, routingSalt string) ([]byte, error) {
	n := cp.quicNonce.Add(1)
	packetNum := uint32(n & 0xFFFF)

	padLen := int(cp.prng.Uint32() % 41)

	// Reuse буфер для inner (plaintext + padding)
	innerLen := 2 + len(plaintext) + padLen
	inner := bufPool.Get().([]byte)[:innerLen]
	defer bufPool.Put(inner[:cap(inner)])

	binary.BigEndian.PutUint16(inner[:2], uint16(len(plaintext)))
	copy(inner[2:], plaintext)
	if padLen > 0 {
		padBuf := padBufPool.Get().([]byte)
		copy(inner[2+len(plaintext):], padBuf[:padLen])
		padBufPool.Put(padBuf)
	}

	nonce := quicNonceFromPacket(cp.key, packetNum)

	ciphertext := cp.gcm.Seal(nil, nonce[:], inner, nil)

	return EncodeQUICHeader(ciphertext, shortID, packetNum, cp.key, routingSalt), nil
}

func (cp *CipherPack) Decrypt(packet []byte) ([]byte, error) {
	if cp.noEncrypt {
		if len(packet) < 2 {
			return nil, fmt.Errorf("packet too short")
		}
		realLen := binary.BigEndian.Uint16(packet[:2])
		if int(realLen)+2 > len(packet) {
			return nil, fmt.Errorf("invalid length")
		}
		return packet[2 : 2+realLen], nil
	}

	if cp.Mode == CipherModeQUIC {
		return cp.decryptQUIC(packet)
	}

	if len(packet) < 4+2+12 {
		return nil, fmt.Errorf("packet too short")
	}

	// Стек-аллоцированные буферы — без heap-alloc
	var marker [4]byte
	copy(marker[:], packet[:4])
	marker[0] &^= 0x40

	var authData [14]byte
	copy(authData[:2], packet[4:6])
	copy(authData[2:], packet[6:18])

	mac := hmac.New(sha256.New, cp.key[:])
	mac.Write(authData[:])
	expected := mac.Sum(nil)[:4]
	expected[0] &^= 0x40
	if !hmac.Equal(marker[:], expected) {
		return nil, fmt.Errorf("HMAC mismatch")
	}

	nonce := packet[6:18]
	ciphertext := packet[18:]

	plaintext, err := cp.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	if len(plaintext) < 2 {
		return nil, fmt.Errorf("payload too short")
	}
	realLen := binary.BigEndian.Uint16(plaintext[:2])
	if int(realLen)+2 > len(plaintext) {
		return nil, fmt.Errorf("invalid length")
	}
	return plaintext[2 : 2+realLen], nil
}

func (cp *CipherPack) decryptQUIC(packet []byte) ([]byte, error) {
	inner, packetNum, err := DecodeQUICHeader(packet, cp.key)
	if err != nil {
		return nil, fmt.Errorf("QUIC header decode: %w", err)
	}

	nonce := quicNonceFromPacket(cp.key, packetNum)

	plaintext, err := cp.gcm.Open(nil, nonce[:], inner, nil)
	if err != nil {
		return nil, fmt.Errorf("GCM decrypt failed: %w", err)
	}
	if len(plaintext) < 2 {
		return nil, fmt.Errorf("payload too short")
	}
	realLen := binary.BigEndian.Uint16(plaintext[:2])
	if int(realLen)+2 > len(plaintext) {
		return nil, fmt.Errorf("invalid length")
	}
	return plaintext[2 : 2+realLen], nil
}

func DeriveKey(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}
