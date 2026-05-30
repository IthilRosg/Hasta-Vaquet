package protocol

import (
	"crypto/aes"
	"encoding/binary"
	"fmt"
	"sync/atomic"
)

var (
	quicSpinBit  atomic.Uint64
	quicKeyPhase atomic.Uint64
)

const quicKeyPhaseInterval = 1000

// EncodeQUICHeader wraps encrypted payload in QUIC Short Header v1 format.
// Output layout (QUIC Short Header + protected payload):
//
//	[0]       = 0x40 | spin(1) | key_phase(1) | reserved(2)
//	[1..4]    = ConnectionID (4 bytes = DynamicID XOR'd with key-derived FNV-1a)
//	[5..6]    = PacketNumber (big-endian 2 bytes)
//	[7+]      = Protected Payload (header-protection XOR mask applied)
//
// Header protection mask: AES(key, packet_number_bytes)[:5]
//   - Byte 0: XOR with mask[0] & 0x1F (preserve 0x40 fixed bit)
//   - Bytes 5-6: XOR with mask[1:3]
//   - Payload bytes: XOR round-robin with mask
func EncodeQUICHeader(encryptedPayload []byte, shortID uint16, packetNum uint32, key [32]byte, routingSalt string) []byte {
	spin := quicSpinBit.Add(1) & 1
	kp := (quicKeyPhase.Add(0) / quicKeyPhaseInterval) & 1

	// Byte 0: fixed 0x40 | spin | key_phase | reserved 0x00
	firstByte := byte(0x40) | byte(spin<<4) | byte(kp<<3)

	// Pre-allocate single buffer for entire packet: [1][connID(4)][pktNum(2)][payload]
	packet := make([]byte, 1+4+2+len(encryptedPayload))

	// ConnectionID mask
	maskVal := Fnv1a16(append(key[:8], byte(shortID), byte(shortID>>8)))

	packet[0] = firstByte
	// ConnectionID bytes directly into packet (no intermediate slice)
	binary.BigEndian.PutUint16(packet[1:3], 0)
	binary.BigEndian.PutUint16(packet[3:5], shortID^maskVal)
	// Packet number bytes directly into packet (no intermediate slice)
	binary.BigEndian.PutUint16(packet[5:7], uint16(packetNum))
	// Payload
	copy(packet[7:], encryptedPayload)

	// Apply header protection: AES(key, packetNumBytes at offset 5)[:5]
	var sample [16]byte
	copy(sample[:2], packet[5:7])
	cipher, _ := aes.NewCipher(key[:])
	cipher.Encrypt(sample[:], sample[:])
	mask := sample[:5]

	// Byte 0: preserve 0x40 fix bit
	packet[0] ^= mask[0] & 0x1F
	// Packet number at bytes 5-6
	packet[5] ^= mask[1]
	packet[6] ^= mask[2]
	// Payload bytes round-robin starting with mask[3]
	for i := 7; i < len(packet); i++ {
		packet[i] ^= mask[(i-7+3)%5]
	}

	return packet
}

// DecodeQUICHeader strips QUIC Short Header and returns the inner encrypted payload,
// along with the decoded packet number.
//
// Stripping reverses header protection using the same AES key:
//  1. Extract packet number bytes from the protected packet
//  2. Decrypt AES sample to get mask
//  3. Un-XOR byte 0, packet number, and payload
//  4. Return inner payload (bytes 7+ after header protection removal)
func DecodeQUICHeader(packet []byte, key [32]byte) ([]byte, uint32, error) {
	if len(packet) < 7 {
		return nil, 0, fmt.Errorf("QUIC header too short: %d < 7", len(packet))
	}

	// Extract the protected packet number bytes (before unprotecting)
	// In real QUIC, we'd need to guess: the packet number length is encoded in
	// the first byte. For simplicity, we assume fixed 2 bytes.
	// The packet number bytes are at offset 5-6 in the protected form.
	protectedPktNum := packet[5:7]

	// Build the AES sample from the protected packet number bytes
	var sample [16]byte
	copy(sample[:2], protectedPktNum)
	cipher, _ := aes.NewCipher(key[:])
	cipher.Encrypt(sample[:], sample[:])
	mask := sample[:5]

	// Un-protect byte 0
	packet[0] ^= mask[0] & 0x1F
	// Un-protect packet number bytes
	packet[5] ^= mask[1]
	packet[6] ^= mask[2]
	// Un-protect payload
	for i := 7; i < len(packet); i++ {
		packet[i] ^= mask[(i-7+3)%5]
	}

	// Verify this is a QUIC Short Header (0x40)
	if packet[0]&0xC0 != 0x40 {
		return nil, 0, fmt.Errorf("not a QUIC short header: first byte=0x%02x", packet[0])
	}

	// Extract packet number from bytes 5-6 (now unprotected)
	packetNum := uint32(binary.BigEndian.Uint16(packet[5:7]))

	// Return inner encrypted payload (bytes 7+)
	inner := make([]byte, len(packet)-7)
	copy(inner, packet[7:])

	return inner, packetNum, nil
}
