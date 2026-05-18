package core

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := DeriveKey("test-secret")
	shortID := uint16(42)
	salt := "test-salt"
	plaintext := []byte("Hello, VPN tunnel! This is a test packet.")

	encrypted, err := Encrypt(plaintext, key[:], shortID, salt)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key[:])
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("Decrypted data doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptTooShort(t *testing.T) {
	key := DeriveKey("test-secret")
	_, err := Decrypt([]byte{0x00, 0x01, 0x02}, key[:])
	if err == nil {
		t.Fatal("Expected error for short packet, got nil")
	}
}

func TestHMACMismatch(t *testing.T) {
	keyA := DeriveKey("key-A")
	keyB := DeriveKey("key-B")
	shortID := uint16(1)
	salt := "salt"

	encrypted, _ := Encrypt([]byte("data"), keyA[:], shortID, salt)
	_, err := Decrypt(encrypted, keyB[:])
	if err == nil {
		t.Fatal("Expected HMAC mismatch, got nil")
	}
}

func TestKeepAliveEncryptDecrypt(t *testing.T) {
	key := DeriveKey("alice-key")
	shortID := uint16(1)
	salt := "HastaVaquetGlobal"

	for i := 0; i < 100; i++ {
		encrypted, err := Encrypt([]byte{}, key[:], shortID, salt)
		if err != nil {
			t.Fatalf("Encrypt empty payload failed: %v", err)
		}
		decrypted, err := Decrypt(encrypted, key[:])
		if err != nil {
			t.Fatalf("Decrypt keep-alive failed: %v", err)
		}
		if len(decrypted) != 0 {
			t.Fatalf("Expected empty decrypted, got %d bytes", len(decrypted))
		}
	}
}

func TestEchoResponse(t *testing.T) {
	key := DeriveKey("echo-key")
	shortID := uint16(2)
	salt := "test"

	echo := []byte{0x01}
	encrypted, _ := Encrypt(echo, key[:], shortID, salt)
	decrypted, err := Decrypt(encrypted, key[:])
	if err != nil {
		t.Fatalf("Decrypt echo failed: %v", err)
	}
	if !bytes.Equal(decrypted, echo) {
		t.Fatalf("Echo mismatch: got %x, want %x", decrypted, echo)
	}
	if len(decrypted) != 1 || decrypted[0] != 0x01 {
		t.Fatalf("Expected single byte 0x01, got %x", decrypted)
	}
}

func TestDynamicIDChanges(t *testing.T) {
	key := DeriveKey("dynamic-key")
	shortID := uint16(99)
	salt := "salt"
	payload := []byte("test")

	var prevDynamicID uint16
	first := true
	for i := 0; i < 50; i++ {
		encrypted, err := Encrypt(payload, key[:], shortID, salt)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}
		dynamicID := (uint16(encrypted[4]) << 8) | uint16(encrypted[5])
		if !first && dynamicID == prevDynamicID {
			t.Fatalf("DynamicID did not change between packets: %d", dynamicID)
		}
		prevDynamicID = dynamicID
		first = false
	}
}

func TestDeriveKey(t *testing.T) {
	k1 := DeriveKey("secret")
	k2 := DeriveKey("secret")
	if k1 != k2 {
		t.Fatal("DeriveKey not deterministic")
	}
	k3 := DeriveKey("different")
	if k1 == k3 {
		t.Fatal("Different secrets produced same key")
	}
}

func TestFnv1a16(t *testing.T) {
	// Basic sanity: non-zero for non-empty input
	h := Fnv1a16([]byte("hello"))
	if h == 0 {
		t.Fatal("Fnv1a16 returned 0 for non-empty input")
	}
	// Deterministic
	h2 := Fnv1a16([]byte("hello"))
	if h != h2 {
		t.Fatal("Fnv1a16 not deterministic")
	}
}

func TestFnv1a64(t *testing.T) {
	h := Fnv1a64([]byte("test"), 0x1234567890ABCDEF)
	if h == 0 {
		t.Fatal("Fnv1a64 returned 0 for non-empty input")
	}
	h2 := Fnv1a64([]byte("test"), 0x1234567890ABCDEF)
	if h != h2 {
		t.Fatal("Fnv1a64 not deterministic")
	}
}
