package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	// Тест 1: UDP через туннель на 10.0.0.2:5203
	fmt.Println("=== Test 1: UDP through tunnel (10.0.0.2:5203) ===")
	testUDP("10.0.0.2:5203")

	// Тест 2: TCP через туннель на 10.0.0.2:5203
	fmt.Println("\n=== Test 2: TCP through tunnel (10.0.0.2:5203) ===")
	testTCP("10.0.0.2:5203")
}

func testUDP(addr string) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		fmt.Println("FAIL dial:", err)
		return
	}
	defer conn.Close()

	payload := make([]byte, 1400)
	n := 10000
	start := time.Now()
	for i := 0; i < n; i++ {
		conn.Write(payload)
	}
	dur := time.Since(start)
	fmt.Printf("Sent %d x 1400B UDP in %v = %.2f Mbps\n", n, dur, float64(n*1400*8)/dur.Seconds()/1e6)
}

func testTCP(addr string) {
	// === UPLOAD ===
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fmt.Println("FAIL TCP dial:", err)
		return
	}

	payload := make([]byte, 65536)
	total := 0
	for time.Since(start) < 3*time.Second {
		n, err := conn.Write(payload)
		if err != nil {
			break
		}
		total += n
	}
	dur := time.Since(start)
	conn.Close()
	fmt.Printf("TCP UPLOAD: %d MB in %.2fs = %.2f Mbps\n", total/1024/1024, dur.Seconds(), float64(total*8)/dur.Seconds()/1e6)

	// === DOWNLOAD ===
	start = time.Now()
	conn2, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fmt.Println("FAIL TCP dial 2:", err)
		return
	}

	buf := make([]byte, 65536)
	total2 := 0
	conn2.SetReadDeadline(time.Now().Add(4 * time.Second))
	for time.Since(start) < 3*time.Second {
		n, err := conn2.Read(buf)
		if err != nil {
			break
		}
		total2 += n
	}
	dur2 := time.Since(start)
	conn2.Close()
	fmt.Printf("TCP DOWNLOAD: %d KB in %.2fs = %.2f Mbps\n", total2/1024, dur2.Seconds(), float64(total2*8)/dur2.Seconds()/1e6)
}
