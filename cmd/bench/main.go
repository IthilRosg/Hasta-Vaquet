// bench — comprehensive mutation benchmark.
// Tests all transport variants, ranks top 3 for speed+obfuscation.
// Usage: go run ./cmd/bench/
package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"hasta-vaquet/core"
	"hasta-vaquet/protocol"
)

const (
	serverIP  = "45.134.39.18"
	udpPort   = 4433
	wsPort    = 4434
	xhttpPort = 4435
	secret    = "ed2ee4b8aa5a7d79a1bf6c78b21356ddf5f79ab44f7e9873"
	shortID   = 3
	salt      = "HastaVaquetGlobal"
	payloadSz = 1400
	durPerRun = 15 * time.Second
)

type TestCfg struct {
	Name      string
	Transport string // "udp" | "quic" | "ws"
	PadMax    int    // 0=stock(40), 200, 512
	FEC       int    // 1=off
	DelayMs   int    // inter-packet delay
	ObfuscLvl int    // 0=none, 1=light, 2=heavy
}

type Result struct {
	Name      string
	Transport string
	UpMbps    float64
	PktPS     float64
	PadMax    int
	FEC       int
	ObfuscLvl int
	Note      string
}

func main() {
	key := protocol.DeriveKey(secret)
	payload := make([]byte, payloadSz)

	cfgs := []TestCfg{
		// ── Phase 1: UDP variations ──
		{Name: "UDP-baseline", Transport: "udp", PadMax: 0, FEC: 1, ObfuscLvl: 0},
		{Name: "UDP-Pad200", Transport: "udp", PadMax: 200, FEC: 1, ObfuscLvl: 1},
		{Name: "UDP-Pad512", Transport: "udp", PadMax: 512, FEC: 1, ObfuscLvl: 1},
		{Name: "UDP-FEC2", Transport: "udp", PadMax: 0, FEC: 2, ObfuscLvl: 0},
		{Name: "UDP-FEC3", Transport: "udp", PadMax: 0, FEC: 3, ObfuscLvl: 0},
		{Name: "UPD-P200-F2", Transport: "udp", PadMax: 200, FEC: 2, ObfuscLvl: 2},
		{Name: "UPD-P512-F2", Transport: "udp", PadMax: 512, FEC: 2, ObfuscLvl: 2},

		// ── Phase 2: QUIC-header variations ──
		{Name: "QUIC-baseline", Transport: "quic", PadMax: 0, FEC: 1, ObfuscLvl: 1},
		{Name: "QUIC-Pad200", Transport: "quic", PadMax: 200, FEC: 1, ObfuscLvl: 1},
		{Name: "QUIC-Pad512", Transport: "quic", PadMax: 512, FEC: 1, ObfuscLvl: 1},
		{Name: "QUIC-FEC2", Transport: "quic", PadMax: 0, FEC: 2, ObfuscLvl: 1},
		{Name: "QUIC-FEC3", Transport: "quic", PadMax: 0, FEC: 3, ObfuscLvl: 1},
		{Name: "QUIC-P200-F2", Transport: "quic", PadMax: 200, FEC: 2, ObfuscLvl: 2},
		{Name: "QUIC-P512-F2", Transport: "quic", PadMax: 512, FEC: 2, ObfuscLvl: 2},

		// ── Phase 3: Delay/noise (ML DPI evasion) ──
		{Name: "Q-Delay5ms", Transport: "quic", PadMax: 0, FEC: 1, ObfuscLvl: 1, DelayMs: 5},
		{Name: "Q-Delay10ms", Transport: "quic", PadMax: 0, FEC: 1, ObfuscLvl: 1, DelayMs: 10},
		{Name: "Q-P200-D5-F2", Transport: "quic", PadMax: 200, FEC: 2, ObfuscLvl: 2, DelayMs: 5},

		// ── Phase 4: WS fallback ──
		{Name: "WS-baseline", Transport: "ws", PadMax: 0, FEC: 1, ObfuscLvl: 0},
		{Name: "WS-Pad200", Transport: "ws", PadMax: 200, FEC: 1, ObfuscLvl: 1},
		{Name: "WS-FEC2", Transport: "ws", PadMax: 0, FEC: 2, ObfuscLvl: 0},

		// ── Phase 5: XHTTP (new) ──
		{Name: "XHTTP-baseline", Transport: "xhttp", PadMax: 0, FEC: 1, ObfuscLvl: 1},
		{Name: "XHTTP-Pad200", Transport: "xhttp", PadMax: 200, FEC: 1, ObfuscLvl: 1},
		{Name: "XHTTP-FEC2", Transport: "xhttp", PadMax: 0, FEC: 2, ObfuscLvl: 1},
	}

	fmt.Println("═╤══════════════════════╤══════════╤════════╤══════╤════════════")
	fmt.Println(" │ Variant              │ Up(Mbps) │ Pkt/s  │ Pad  │ Obfusc")
	fmt.Println("═╪══════════════════════╪══════════╪════════╪══════╪════════════")

	var all []Result
	for _, cfg := range cfgs {
		r := runSingle(cfg, key, payload)
		all = append(all, r)
		obf := map[int]string{0: "—", 1: "light", 2: "heavy"}[r.ObfuscLvl]
		fmt.Printf(" │ %-20s │ %8.0f │ %6.0f │ %4d │ %s\n",
			r.Name, r.UpMbps, r.PktPS, r.PadMax, obf)
	}
	fmt.Println("═╧══════════════════════╧══════════╧════════╧══════╧════════════")

	// ── Rank by speed ──
	sort.Slice(all, func(i, j int) bool { return all[i].UpMbps > all[j].UpMbps })

	fmt.Println("\n═══════════ TOP 5 BY SPEED ═══════════")
	for i, r := range all[:5] {
		fmt.Printf("  #%d: %s — %.0f Mbps (pad=%d, fec=%d)\n", i+1, r.Name, r.UpMbps, r.PadMax, r.FEC)
	}

	// ── Top 3 with obfuscation (light or heavy) ──
	var obfusc []Result
	for _, r := range all {
		if r.ObfuscLvl >= 1 {
			obfusc = append(obfusc, r)
		}
	}
	sort.Slice(obfusc, func(i, j int) bool { return obfusc[i].UpMbps > obfusc[j].UpMbps })

	fmt.Println("\n═══════════ TOP 3 (SPEED + OBFUSCATION) ═══════════")
	for i, r := range obfusc[:3] {
		lvl := map[int]string{0: "none", 1: "light", 2: "heavy"}[r.ObfuscLvl]
		fmt.Printf("  🥇%d: %s — %.0f Mbps (obfusc=%s, pad=%d, fec=%d)\n",
			i+1, r.Name, r.UpMbps, lvl, r.PadMax, r.FEC)
	}

	fmt.Println("\n═══════════ CATEGORY LEADERS ═══════════")
	fmt.Println("  🎮 Games (speed):", all[0].Name, all[0].UpMbps, "Mbps")
	fmt.Println("  🛡 DPI evasion:", obfusc[0].Name, obfusc[0].UpMbps, "Mbps")
	fmt.Println("  🔄 Fallback (WS):", func() string {
		for _, r := range all {
			if r.Transport == "ws" {
				return fmt.Sprintf("%s — %.0f Mbps", r.Name, r.UpMbps)
			}
		}
		return ""
	}())
}

func runSingle(cfg TestCfg, key [32]byte, payload []byte) Result {
	ctx := context.Background()

	encCP, err := protocol.NewCipherPack(key[:])
	if err != nil {
		return Result{Name: cfg.Name, Note: "ERR:cp"}
	}
	encCP.PadMax = cfg.PadMax

	conn, err := dialTransport(ctx, cfg.Transport)
	if err != nil {
		return Result{Name: cfg.Name, Note: "ERR:dial"}
	}
	defer conn.Close()

	dataPkt, err := encCP.Encrypt(payload, shortID, salt)
	if err != nil {
		return Result{Name: cfg.Name, Note: "ERR:enc"}
	}

	var sentBytes, sentPkts int64
	start := time.Now()

	for time.Since(start) < durPerRun {
		fec := clamp(cfg.FEC, 1, 5)
		for i := 0; i < fec; i++ {
			n, err := conn.Write(dataPkt)
			if err == nil {
				sentBytes += int64(n)
			}
		}
		sentPkts++

		if cfg.DelayMs > 0 {
			time.Sleep(time.Duration(cfg.DelayMs) * time.Millisecond)
		}
	}

	dur := time.Since(start).Seconds()
	wireMbps := float64(sentBytes*8) / dur / 1e6
	pktLen := len(dataPkt)
	upMbps := wireMbps / float64(pktLen) * float64(payloadSz) / float64(fecMax(cfg.FEC))
	pktps := float64(sentPkts) / dur

	return Result{
		Name:      cfg.Name,
		Transport: cfg.Transport,
		UpMbps:    upMbps,
		PktPS:     pktps,
		PadMax:    cfg.PadMax,
		FEC:       cfg.FEC,
		ObfuscLvl: cfg.ObfuscLvl,
	}
}

func dialTransport(ctx context.Context, t string) (interface {
	Write([]byte) (int, error)
	Close() error
}, error) {
	switch t {
	case "quic":
		return core.DialQUICUDP(ctx, serverIP, udpPort, shortID, secret)
	case "xhttp":
		return core.DialXHTTP(ctx, serverIP, xhttpPort)
	case "ws":
		return core.DialWS(ctx, serverIP, wsPort)
	default:
		return core.DialRawUDP(ctx, serverIP, udpPort)
	}
}

func fecMax(fec int) int { return clamp(fec, 1, 5) }
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
