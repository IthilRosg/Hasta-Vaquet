// echotest — диагностика keep-alive → echo цикла.
package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"hasta-vaquet/core"
	"hasta-vaquet/protocol"
)

const (
	serverIP = "45.134.39.18"
	port     = 4433
	wsPort   = 4434
	secret   = "ed2ee4b8aa5a7d79a1bf6c78b21356ddf5f79ab44f7e9873"
	shortID  = 3
	salt     = "HastaVaquetGlobal"
)

func main() {
	key := protocol.DeriveKey(secret)

	fmt.Println("=== Echo Test 0: Raw UDP connectivity ===")
	testRawUDP()

	fmt.Println("\n=== Echo Test 1: Encrypted keep-alive (Raw UDP) ===")
	testEchoUDP(key)

	fmt.Println("\n=== Echo Test 2: Encrypted keep-alive (QUIC-header) ===")
	testEchoQUIC(key)

	fmt.Println("\n=== Echo Test 3: WS (TCP fallback) ===")
	testEchoWS(key)

	fmt.Println("\n=== Done ===")
}

func testRawUDP() {
	conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", serverIP, port))
	if err != nil {
		fmt.Printf("FAIL dial: %v\n", err)
		return
	}
	defer conn.Close()
	fmt.Printf("Connected: %s -> %s\n", conn.LocalAddr(), conn.RemoteAddr())
	n, err := conn.Write([]byte{0x01})
	if err != nil {
		fmt.Printf("FAIL write: %v\n", err)
		return
	}
	fmt.Printf("Sent %d bytes\n", n)
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			fmt.Printf("Timeout — ожидаемо (сервер дропает мусор)\n")
		} else {
			fmt.Printf("Read error: %v\n", err)
		}
		return
	}
	fmt.Printf("Received %d bytes: %x\n", n, buf[:n])
}

func echoTest(cp *protocol.CipherPack, ka []byte, conn net.Conn, label string) {
	fmt.Printf("Keep-alive packet size: %d bytes\n", len(ka))
	fmt.Printf("  marker[0]=0x%02x (0x40 bit=%v)\n", ka[0], ka[0]&0x40 != 0)

	n, err := conn.Write(ka)
	if err != nil {
		fmt.Printf("FAIL write: %v\n", err)
		return
	}
	fmt.Printf("Sent %d bytes\n", n)

	buf := make([]byte, 65535)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			fmt.Printf("TIMEOUT: no echo received in 5s\n")
			return
		}
		fmt.Printf("FAIL read: %v\n", err)
		return
	}
	fmt.Printf("Received %d bytes\n", n)

	if n < 4+2+12 {
		fmt.Printf("  TOO SHORT (%d < 18)\n", n)
		fmt.Printf("  raw: %x\n", buf[:n])
		return
	}

	dec, err := cp.Decrypt(buf[:n])
	if err != nil {
		fmt.Printf("  DECRYPT FAIL: %v\n", err)
		return
	}
	fmt.Printf("  Decrypted %d bytes: %x\n", len(dec), dec)

	if len(dec) == 1 && dec[0] == 0x01 {
		fmt.Printf("  ✅ ECHO RECEIVED!\n")
	} else {
		fmt.Printf("  ❌ Not echo (expected 01, got %x)\n", dec)
	}
}

func testEchoUDP(key [32]byte) {
	cp, _ := protocol.NewCipherPack(key[:])
	ka, _ := cp.Encrypt([]byte{}, shortID, salt)
	conn, err := core.DialRawUDP(context.Background(), serverIP, port)
	if err != nil {
		fmt.Printf("FAIL dial: %v\n", err)
		return
	}
	defer conn.Close()
	fmt.Printf("Connected: %s -> %s\n", conn.LocalAddr(), conn.RemoteAddr())
	echoTest(cp, ka, conn, "UDP")
}

func testEchoQUIC(key [32]byte) {
	cp, _ := protocol.NewCipherPack(key[:])
	ka, _ := cp.Encrypt([]byte{}, shortID, salt)
	conn, err := core.DialQUICUDP(context.Background(), serverIP, port, shortID, secret)
	if err != nil {
		fmt.Printf("FAIL dial: %v\n", err)
		return
	}
	defer conn.Close()
	fmt.Printf("Connected: %s -> %s\n", conn.LocalAddr(), conn.RemoteAddr())
	echoTest(cp, ka, conn, "QUIC")
}

func testEchoWS(key [32]byte) {
	cp, _ := protocol.NewCipherPack(key[:])
	ka, _ := cp.Encrypt([]byte{}, shortID, salt)
	conn, err := core.DialWS(context.Background(), serverIP, wsPort)
	if err != nil {
		fmt.Printf("FAIL dial: %v\n", err)
		return
	}
	defer conn.Close()
	fmt.Printf("Connected: %s -> %s\n", conn.LocalAddr(), conn.RemoteAddr())
	echoTest(cp, ka, conn, "WS")
}
