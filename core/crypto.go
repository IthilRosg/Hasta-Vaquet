package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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

type CipherPack struct {
	key  [32]byte
	gcm  cipher.AEAD
	rbuf [64]byte
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
	return &CipherPack{key: k, gcm: gcm}, nil
}

func (cp *CipherPack) Encrypt(plaintext []byte, shortID uint16, routingSalt string) ([]byte, error) {
	// Single rand.Read for nonce + padByte + padding data (up to 40 bytes)
	if _, err := rand.Read(cp.rbuf[:]); err != nil {
		return nil, fmt.Errorf("encrypt: rand: %w", err)
	}

	nonce := cp.rbuf[:12]
	padLen := int(cp.rbuf[12]) % 41

	inner := make([]byte, 2+len(plaintext)+padLen)
	binary.BigEndian.PutUint16(inner[:2], uint16(len(plaintext)))
	copy(inner[2:], plaintext)
	if padLen > 0 {
		copy(inner[2+len(plaintext):], cp.rbuf[13:13+padLen])
	}

	routeMask := Fnv1a16(append([]byte(routingSalt), nonce...))
	dynamicID := shortID ^ routeMask

	authData := make([]byte, 14)
	binary.BigEndian.PutUint16(authData[:2], dynamicID)
	copy(authData[2:], nonce)

	mac := hmac.New(sha256.New, cp.key[:])
	mac.Write(authData)
	marker := mac.Sum(nil)[:4]
	marker[0] |= 0x40

	ciphertext := cp.gcm.Seal(nil, nonce, inner, nil)

	buf := make([]byte, 4+2+12+len(ciphertext))
	copy(buf[:4], marker)
	binary.BigEndian.PutUint16(buf[4:6], dynamicID)
	copy(buf[6:18], nonce)
	copy(buf[18:], ciphertext)
	return buf, nil
}

func (cp *CipherPack) Decrypt(packet []byte) ([]byte, error) {
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

func DeriveKey(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}
