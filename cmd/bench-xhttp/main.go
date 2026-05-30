package main

import (
	"context"
	"fmt"
	"time"

	"hasta-vaquet/core"
	"hasta-vaquet/protocol"
)

func main() {
	key := protocol.DeriveKey("ed2ee4b8aa5a7d79a1bf6c78b21356ddf5f79ab44f7e9873")
	cp, _ := protocol.NewCipherPack(key[:])

	// Тест 1: Echo
	fmt.Println("=== Test 1: XHTTP Echo ===")
	ka, _ := cp.Encrypt([]byte{}, 3, "HastaVaquetGlobal")
	conn, err := core.DialXHTTP(context.Background(), "45.134.39.18", 4435)
	if err != nil {
		fmt.Println("DIAL ERR:", err)
		return
	}
	defer conn.Close()
	fmt.Println("Connected, sending keep-alive...")
	n, err := conn.Write(ka)
	fmt.Println("Write:", n, err)
	buf := make([]byte, 200)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		fmt.Println("READ ERR:", err)
		return
	}
	dec, _ := cp.Decrypt(buf[:n])
	if len(dec) == 1 && dec[0] == 0x01 {
		fmt.Println("✅ XHTTP ECHO OK")
	} else {
		fmt.Println("BAD ECHO:", dec)
	}
	conn.Close()

	// Тест 2: Throughput
	fmt.Println("\n=== Test 2: XHTTP Throughput ===")
	payload := make([]byte, 1400)
	cp2, _ := protocol.NewCipherPack(key[:])
	dataPkt, _ := cp2.Encrypt(payload, 3, "HastaVaquetGlobal")
	conn2, err := core.DialXHTTP(context.Background(), "45.134.39.18", 4435)
	if err != nil {
		fmt.Println("DIAL ERR:", err)
		return
	}
	defer conn2.Close()

	var sentBytes, sentPkts int64
	start := time.Now()
	for time.Since(start) < 10*time.Second {
		n, err := conn2.Write(dataPkt)
		if err == nil {
			sentBytes += int64(n)
		}
		sentPkts++
		if sentPkts%1000 == 0 {
			fmt.Printf("  %d pkts sent...\n", sentPkts)
		}
	}
	dur := time.Since(start).Seconds()
	wireMbps := float64(sentBytes*8) / dur / 1e6
	upMbps := wireMbps / float64(len(dataPkt)) * float64(1400)
	fmt.Printf("\nXHTTP throughput: %.0f Mbps (%d pkt/s)\n", upMbps, int64(float64(sentPkts)/dur))
	conn2.Close()
}
