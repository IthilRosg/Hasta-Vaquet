// Package protocol — криптографический слой Hasta-Vaquet.
// cipher.go: шифрование, HMAC, nonce, padding.
package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

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
// Безопасен для конкурентного доступа (mutex на GCM).
type CipherPack struct {
	key       [32]byte
	gcm       cipher.AEAD
	mu        sync.Mutex    // защита gcm.Seal/Open (AEAD не thread-safe)
	nonce     atomic.Uint64 // monotonic counter
	prng      *rand.Rand    // быстрый PRNG для padding
	noEncrypt bool          // true = Encrypt/Decrypt без крипты (для замера накладных расходов)
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
	seed := int64(k[0])<<56 | int64(k[1])<<48 | int64(time.Now().UnixNano())
	prng := rand.New(rand.NewSource(seed))
	cp := &CipherPack{
		key:  k,
		gcm:  gcm,
		prng: prng,
	}
	cp.nonce.Store(prng.Uint64())
	return cp, nil
}

func NewNoopCipherPack() *CipherPack {
	return &CipherPack{noEncrypt: true}
}

func (cp *CipherPack) Encrypt(plaintext []byte, shortID uint16, routingSalt string) ([]byte, error) {
	if cp.noEncrypt {
		// Без шифрования: только 2 байта длины + plaintext
		buf := make([]byte, 2+len(plaintext))
		binary.BigEndian.PutUint16(buf[:2], uint16(len(plaintext)))
		copy(buf[2:], plaintext)
		return buf, nil
	}

	n := cp.nonce.Add(1)
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[:8], n)
	binary.BigEndian.PutUint32(nonce[8:], cp.prng.Uint32())

	padLen := int(cp.prng.Int31n(41))

	inner := make([]byte, 2+len(plaintext)+padLen)
	binary.BigEndian.PutUint16(inner[:2], uint16(len(plaintext)))
	copy(inner[2:], plaintext)
	if padLen > 0 {
		cp.prng.Read(inner[2+len(plaintext):])
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

	cp.mu.Lock()
	ciphertext := cp.gcm.Seal(nil, nonce[:], inner, nil)
	cp.mu.Unlock()

	buf := make([]byte, 4+2+12+len(ciphertext))
	copy(buf[:4], marker)
	binary.BigEndian.PutUint16(buf[4:6], dynamicID)
	copy(buf[6:18], nonce[:])
	copy(buf[18:], ciphertext)
	return buf, nil
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

	if len(packet) < 4+2+12 {
		return nil, fmt.Errorf("packet too short")
	}

	marker := make([]byte, 4)
	copy(marker, packet[:4])
	marker[0] &^= 0x40
	nonce := packet[6:18]
	ciphertext := packet[18:]

	authData := make([]byte, 14)
	copy(authData[:2], packet[4:6])
	copy(authData[2:], nonce)

	mac := hmac.New(sha256.New, cp.key[:])
	mac.Write(authData)
	expected := mac.Sum(nil)[:4]
	expected[0] &^= 0x40
	if !hmac.Equal(marker, expected) {
		return nil, fmt.Errorf("HMAC mismatch")
	}

	cp.mu.Lock()
	plaintext, err := cp.gcm.Open(nil, nonce, ciphertext, nil)
	cp.mu.Unlock()
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

func DeriveKey(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}
