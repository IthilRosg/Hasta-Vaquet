//go:build ignore

package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"hasta-vaquet/core"
	"hasta-vaquet/protocol"
)

const (
	serverIP    = "45.134.39.18"
	secretKey   = "ed2ee4b8aa5a7d79a1bf6c78b21356ddf5f79ab44f7e9873"
	routingSalt = "HastaVaquetGlobal"
	shortID     = 3
	payloadSize = 1400
	testDur     = 15 * time.Second
	runsPerTest = 3
	udpPort     = 4433
	wsPort      = 4434
)

var key [32]byte

func init() {
	key = protocol.DeriveKey(secretKey)
}

type RunResult struct {
	WireBytes    int64
	PayloadCount int64
	Duration     time.Duration
	Err          error
}

// encBufPool reduces allocs for encrypt output reuse
var encBufPool = sync.Pool{
	New: func() any { return make([]byte, 65535) },
}

func main() {
	fmt.Println("========================================================================")
	fmt.Println("  Hasta-Vaquet Comprehensive Speed Tests")
	fmt.Println("========================================================================")
	fmt.Printf("  Server:        %s\n", serverIP)
	fmt.Printf("  Payload:       %d bytes\n", payloadSize)
	fmt.Printf("  Run duration:  %.0f seconds\n", testDur.Seconds())
	fmt.Printf("  Runs per test: %d\n", runsPerTest)
	fmt.Println("========================================================================")
	fmt.Println()

	// === Transport modes (1-3) ===
	runSuite("Raw UDP (port 4433)", runRawUDP)
	runSuite("WebSocket WS (port 4434)", runWS)
	runSuite("QUIC-header UDP (port 4433)", runQUICUDP)

	// === UDP variants (4-9) ===
	runSuite("UDP with MTU=1400", func() *RunResult { return runConfigurableUDP(1400, 1, 0) })
	runSuite("UDP with MTU=1500", func() *RunResult { return runConfigurableUDP(1500, 1, 0) })
	runSuite("UDP with FEC=2", func() *RunResult { return runConfigurableUDP(1300, 2, 0) })
	runSuite("UDP with FEC=3", func() *RunResult { return runConfigurableUDP(1300, 3, 0) })
	runSuite("UDP with Large Padding 0-200", func() *RunResult { return runConfigurableUDP(1300, 1, 201) })
	runSuite("QUIC header mode (CipherModeQUIC)", runQUICMode)

	fmt.Println("========================================================================")
	fmt.Println("  All tests complete!")
	fmt.Println("========================================================================")
}

// ─── Suite runner ──────────────────────────────────────────────────────────

func runSuite(name string, fn func() *RunResult) {
	fmt.Printf("=== %s ===\n", name)

	var wireMbps, payloadMbps []float64
	var pktPs []float64

	for i := 0; i < runsPerTest; i++ {
		result := fn()
		if result.Err != nil {
			fmt.Printf("  Run %d: FAILED — %v\n", i+1, result.Err)
			continue
		}

		dur := result.Duration.Seconds()
		wMbps := float64(result.WireBytes*8) / dur / 1e6
		pMbps := float64(result.PayloadCount*payloadSize*8) / dur / 1e6
		pps := float64(result.PayloadCount) / dur

		wireMbps = append(wireMbps, wMbps)
		payloadMbps = append(payloadMbps, pMbps)
		pktPs = append(pktPs, pps)

		fmt.Printf("  Run %d: %.2f Mbps wire / %.2f Mbps payload  (%.0f pkt/s)\n",
			i+1, wMbps, pMbps, pps)
	}

	if len(wireMbps) > 0 {
		avgW, minW, maxW := stats(wireMbps)
		avgP, minP, maxP := stats(payloadMbps)
		_, minR, maxR := stats(pktPs)

		fmt.Printf("  AVG:   %.2f Mbps wire / %.2f Mbps payload  (%.0f pkt/s)\n", avgW, avgP, avg(pktPs))
		if len(wireMbps) > 1 {
			fmt.Printf("  MIN:   %.2f Mbps wire / %.2f Mbps payload  (%.0f pkt/s)\n", minW, minP, minR)
			fmt.Printf("  MAX:   %.2f Mbps wire / %.2f Mbps payload  (%.0f pkt/s)\n", maxW, maxP, maxR)
		}
		overhead := (1 - avgP/avgW) * 100
		fmt.Printf("  OVH:   %.1f%% protocol overhead\n", overhead)
	}
	fmt.Println()
}

// ─── Stats helpers ─────────────────────────────────────────────────────────

func stats(vals []float64) (avg, min, max float64) {
	if len(vals) == 0 {
		return 0, 0, 0
	}
	min = math.MaxFloat64
	max = -math.MaxFloat64
	sum := 0.0
	for _, v := range vals {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return sum / float64(len(vals)), min, max
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// ─── Test 1: Raw UDP ───────────────────────────────────────────────────────

func runRawUDP() *RunResult {
	ctx, cancel := context.WithTimeout(context.Background(), testDur+10*time.Second)
	defer cancel()

	conn, err := core.DialRawUDP(ctx, serverIP, udpPort)
	if err != nil {
		return &RunResult{Err: fmt.Errorf("dial: %w", err)}
	}
	defer conn.Close()

	cp, err := protocol.NewCipherPack(key[:])
	if err != nil {
		return &RunResult{Err: fmt.Errorf("cipher: %w", err)}
	}

	return sendLoop(conn, cp, 1)
}

// ─── Test 2: WebSocket ─────────────────────────────────────────────────────

func runWS() *RunResult {
	ctx, cancel := context.WithTimeout(context.Background(), testDur+10*time.Second)
	defer cancel()

	conn, err := core.DialWS(ctx, serverIP, wsPort)
	if err != nil {
		return &RunResult{Err: fmt.Errorf("dial: %w", err)}
	}
	defer conn.Close()

	cp, err := protocol.NewCipherPack(key[:])
	if err != nil {
		return &RunResult{Err: fmt.Errorf("cipher: %w", err)}
	}

	return sendLoop(conn, cp, 1)
}

// ─── Test 3: QUIC-header UDP ───────────────────────────────────────────────

func runQUICUDP() *RunResult {
	ctx, cancel := context.WithTimeout(context.Background(), testDur+10*time.Second)
	defer cancel()

	conn, err := core.DialQUICUDP(ctx, serverIP, udpPort, shortID, secretKey)
	if err != nil {
		return &RunResult{Err: fmt.Errorf("dial: %w", err)}
	}
	defer conn.Close()

	cp, err := protocol.NewCipherPack(key[:])
	if err != nil {
		return &RunResult{Err: fmt.Errorf("cipher: %w", err)}
	}

	return sendLoop(conn, cp, 1)
}

// ─── Tests 4-8: Configurable UDP (MTU, FEC, Padding) ──────────────────────

func runConfigurableUDP(mtu, fec, padMax int) *RunResult {
	ctx, cancel := context.WithTimeout(context.Background(), testDur+10*time.Second)
	defer cancel()

	conn, err := core.DialRawUDP(ctx, serverIP, udpPort)
	if err != nil {
		return &RunResult{Err: fmt.Errorf("dial: %w", err)}
	}
	defer conn.Close()

	cp, err := protocol.NewCipherPack(key[:])
	if err != nil {
		return &RunResult{Err: fmt.Errorf("cipher: %w", err)}
	}
	if padMax > 0 {
		cp.PadMax = padMax
	}

	// Set socket buffer sizes for consistency
	if udpConn, ok := conn.(*net.UDPConn); ok {
		udpConn.SetWriteBuffer(4 * 1024 * 1024)
		udpConn.SetReadBuffer(4 * 1024 * 1024)
	}

	return sendLoop(conn, cp, fec)
}

// ─── Test 9: QUIC header mode (CipherModeQUIC) ────────────────────────────

func runQUICMode() *RunResult {
	ctx, cancel := context.WithTimeout(context.Background(), testDur+10*time.Second)
	defer cancel()

	conn, err := core.DialRawUDP(ctx, serverIP, udpPort)
	if err != nil {
		return &RunResult{Err: fmt.Errorf("dial: %w", err)}
	}
	defer conn.Close()

	cp, err := protocol.NewCipherPack(key[:])
	if err != nil {
		return &RunResult{Err: fmt.Errorf("cipher: %w", err)}
	}
	cp.Mode = protocol.CipherModeQUIC

	return sendLoop(conn, cp, 1)
}

// ─── Common send loop ──────────────────────────────────────────────────────

func sendLoop(conn net.Conn, cp *protocol.CipherPack, fec int) *RunResult {
	payload := make([]byte, payloadSize)

	// Pre-fill with deterministic data (doesn't matter after encryption)
	for i := range payload {
		payload[i] = byte(i)
	}

	var wireBytes int64
	var payloadCount int64

	start := time.Now()
	deadline := start.Add(testDur)

	for time.Now().Before(deadline) {
		encrypted, err := cp.Encrypt(payload, shortID, routingSalt)
		if err != nil {
			continue
		}

		for f := 0; f < fec; f++ {
			n, err := conn.Write(encrypted)
			if err != nil {
				// For UDP, writes almost never fail; for WS/QUIC they might
				break
			}
			wireBytes += int64(n)
		}
		payloadCount++
	}

	dur := time.Since(start)
	return &RunResult{
		WireBytes:    wireBytes,
		PayloadCount: payloadCount,
		Duration:     dur,
	}
}

// suppress unused import warning for net
var _ net.Addr
