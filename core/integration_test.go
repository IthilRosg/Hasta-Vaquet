package core

import (
	"bytes"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type fakeServer struct {
	key       [32]byte
	rxCount   atomic.Int64
	lastNonce []byte
	conn      *net.UDPConn
	done      chan struct{}
}

func (s *fakeServer) start(addr string) error {
	udpAddr, _ := net.ResolveUDPAddr("udp", addr)
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	s.conn = conn
	s.done = make(chan struct{})
	go func() {
		buf := make([]byte, 65535)
		for {
			select {
			case <-s.done:
				return
			default:
			}
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil || n < 4+2+12 {
				continue
			}
			decrypted, err := Decrypt(buf[:n], s.key[:])
			if err != nil {
				continue
			}
			s.rxCount.Add(1)
			s.lastNonce = make([]byte, 12)
			copy(s.lastNonce, buf[6:18])
			if len(decrypted) == 0 {
				enc, _ := Encrypt([]byte{0x01}, s.key[:], 1, "test-salt")
				conn.WriteToUDP(enc, clientAddr)
			}
		}
	}()
	return nil
}

func (s *fakeServer) stop() {
	close(s.done)
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *fakeServer) addr() *net.UDPAddr {
	return s.conn.LocalAddr().(*net.UDPAddr)
}

func dialServer(srv *fakeServer) (*net.UDPConn, error) {
	return net.DialUDP("udp", nil, srv.addr())
}

func TestIntegrationLocalUDP(t *testing.T) {
	sharedKey := DeriveKey("integration-test-shared")

	srv := &fakeServer{key: sharedKey}
	if err := srv.start("127.0.0.1:0"); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.stop()

	conn, err := dialServer(srv)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer conn.Close()

	payload := []byte("Hello VPN server from integration test!")
	encrypted, err := Encrypt(payload, sharedKey[:], 1, "test-salt")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := conn.Write(encrypted); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := srv.rxCount.Load(); n != 1 {
		t.Fatalf("expected 1 packet on server, got %d", n)
	}

	emptyEnc, _ := Encrypt([]byte{}, sharedKey[:], 1, "test-salt")
	conn.Write(emptyEnc)

	buf := make([]byte, 65535)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read echo response: %v", err)
	}
	echoDecrypted, err := Decrypt(buf[:n], sharedKey[:])
	if err != nil {
		t.Fatalf("decrypt echo: %v", err)
	}
	if len(echoDecrypted) != 1 || echoDecrypted[0] != 0x01 {
		t.Fatalf("expected echo 0x01, got %x", echoDecrypted)
	}

	wrongKey := DeriveKey("wrong-key")
	wrongKeyEnc, _ := Encrypt(payload, wrongKey[:], 2, "test-salt")
	_, err = Decrypt(wrongKeyEnc, sharedKey[:])
	if err == nil {
		t.Fatal("expected HMAC mismatch with wrong key")
	}
}

func TestIntegrationMultipleKeepAlives(t *testing.T) {
	srv := &fakeServer{key: DeriveKey("ka-key")}
	if err := srv.start("127.0.0.1:0"); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.stop()

	conn, err := dialServer(srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := DeriveKey("ka-key")
	for i := 0; i < 15; i++ {
		enc, _ := Encrypt([]byte{}, key[:], 1, "test-salt")
		conn.Write(enc)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if n := srv.rxCount.Load(); n != 15 {
		t.Fatalf("expected 15 keep-alives, got %d", n)
	}
}

func TestIntegrationEchoRTT(t *testing.T) {
	srv := &fakeServer{key: DeriveKey("echo-rtt-key")}
	if err := srv.start("127.0.0.1:0"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.stop()

	conn, err := dialServer(srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := DeriveKey("echo-rtt-key")
	start := time.Now()
	enc, _ := Encrypt([]byte{}, key[:], 1, "test-salt")
	conn.Write(enc)

	buf := make([]byte, 65535)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	rtt := time.Since(start)
	decrypted, _ := Decrypt(buf[:n], key[:])
	if len(decrypted) != 1 || decrypted[0] != 0x01 {
		t.Fatalf("bad echo: %x", decrypted)
	}
	if rtt > 500*time.Millisecond {
		t.Fatalf("RTT too high: %v", rtt)
	}
	t.Logf("Local RTT: %v", rtt)
}

func TestIntegrationLargePacket(t *testing.T) {
	srv := &fakeServer{key: DeriveKey("large-pkt")}
	if err := srv.start("127.0.0.1:0"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.stop()

	conn, err := dialServer(srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := DeriveKey("large-pkt")
	payload := bytes.Repeat([]byte("A"), 1100)

	enc, _ := Encrypt(payload, key[:], 1, "test-salt")
	conn.Write(enc)

	time.Sleep(100 * time.Millisecond)
	if n := srv.rxCount.Load(); n != 1 {
		t.Fatalf("expected 1 large packet, got %d", n)
	}
}

func TestIntegrationConcurrentPackets(t *testing.T) {
	srv := &fakeServer{key: DeriveKey("concurrent")}
	if err := srv.start("127.0.0.1:0"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.stop()

	conn, err := dialServer(srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := DeriveKey("concurrent")
	const count = 50
	for i := 0; i < count; i++ {
		payload := []byte{byte(i)}
		enc, _ := Encrypt(payload, key[:], 1, "test-salt")
		conn.Write(enc)
	}

	time.Sleep(200 * time.Millisecond)
	if n := srv.rxCount.Load(); n != int64(count) {
		t.Fatalf("expected %d packets, got %d", count, n)
	}
}

type testListener struct {
	statuses    []string
	reconnectingCh chan string
}

func (l *testListener) OnStatus(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64) {
	l.statuses = append(l.statuses, status)
	if status == "reconnecting" && l.reconnectingCh != nil {
		select {
		case l.reconnectingCh <- status:
		default:
		}
	}
}

func TestReconnectStateMachine(t *testing.T) {
	listener := &testListener{reconnectingCh: make(chan string, 10)}
	cfg := Config{
		ServerIP:    "127.0.0.1",
		Port:        19999,
		ShortID:     1,
		SecretKey:   "reconnect-test-key",
		RoutingSalt: "test-salt",
		InternalIP:  "10.0.0.99",
		GatewayIP:   "192.168.100.1",
		DNS:         "1.1.1.1",
	}
	vpn := New(cfg, listener)
	vpn.running.Store(true)
	vpn.stopCh = make(chan struct{})

	go vpn.enterReconnecting()

	select {
	case <-listener.reconnectingCh:
		t.Log("enterReconnecting: callback('reconnecting') received")
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for reconnect callback")
	}

	if !vpn.reconnecting.Load() {
		t.Fatal("reconnecting flag should be true")
	}

	// running остаётся true — TUN не закрывается
	if !vpn.running.Load() {
		t.Fatal("running should stay true (TUN kept alive)")
	}

	// Stop во время reconnect
	vpn.Stop()

	time.Sleep(200 * time.Millisecond)
	if vpn.reconnecting.Load() {
		t.Fatal("reconnecting should be false after Stop()")
	}
}

func TestReconnectStopCancels(t *testing.T) {
	listener := &testListener{reconnectingCh: make(chan string, 10)}
	cfg := Config{
		ServerIP:    "127.0.0.1",
		Port:        19998,
		ShortID:     1,
		SecretKey:   "cancel-key",
		RoutingSalt: "test-salt",
		InternalIP:  "10.0.0.98",
		GatewayIP:   "192.168.100.1",
		DNS:         "1.1.1.1",
	}
	vpn := New(cfg, listener)
	vpn.running.Store(true)
	vpn.stopCh = make(chan struct{})

	go vpn.enterReconnecting()

	<-listener.reconnectingCh
	t.Log("Reconnect started")

	// Stop during reconnect
	go vpn.Stop()
	time.Sleep(200 * time.Millisecond)

	if vpn.reconnecting.Load() {
		t.Fatal("reconnecting should be false after Stop()")
	}
}

func TestReconnectDoubleCallIsSafe(t *testing.T) {
	listener := &testListener{reconnectingCh: make(chan string, 10)}
	vpn := New(Config{
		ServerIP: "127.0.0.1", Port: 19997, ShortID: 1,
		SecretKey: "double-key", RoutingSalt: "salt",
		InternalIP: "10.0.0.97",
	}, listener)
	vpn.running.Store(true)
	vpn.stopCh = make(chan struct{})

	// Call enterReconnecting twice in parallel
	go vpn.enterReconnecting()
	go vpn.enterReconnecting()

	// Should only get one reconnect
	count := 0
	select {
	case <-listener.reconnectingCh:
		count++
	case <-time.After(3 * time.Second):
	}
	select {
	case <-listener.reconnectingCh:
		count++
	case <-time.After(200 * time.Millisecond):
	}

	if count > 1 {
		t.Fatal("enterReconnecting should only execute once")
	}
}

func TestExitReconnectingResetsState(t *testing.T) {
	listener := &testListener{reconnectingCh: make(chan string, 10)}
	vpn := New(Config{
		ServerIP: "127.0.0.1", Port: 19995, ShortID: 1,
		SecretKey: "exit-key", RoutingSalt: "salt",
		InternalIP: "10.0.0.95",
	}, listener)
	vpn.running.Store(true)
	vpn.stopCh = make(chan struct{})

	vpn.enterReconnecting()

	if !vpn.reconnecting.Load() {
		t.Fatal("reconnecting should be true after enterReconnecting")
	}
	if vpn.readFails.Load() != 0 {
		t.Fatal("readFails should be 0 initially")
	}

	vpn.readFails.Store(5)
	vpn.exitReconnecting()

	if vpn.reconnecting.Load() {
		t.Fatal("reconnecting should be false after exitReconnecting")
	}
	if vpn.readFails.Load() != 0 {
		t.Fatal("readFails should be reset to 0 after exitReconnecting")
	}
}

func TestKillSwitchHooksNoPanic(t *testing.T) {
	// Этот тест проверяет что activateKillSwitch сначала добавляет route до сервера
	// прежде чем удалить default route. На этой платформе — только проверка вызова
	// (реальная имплементация в vpn_windows.go)

	vpn := New(Config{
		ServerIP: "31.42.120.154", Port: 9999, ShortID: 1,
		SecretKey: "ks-route", RoutingSalt: "salt",
		InternalIP: "10.0.0.1", GatewayIP: "192.168.1.1",
	}, &testListener{})

	// Симулируем kill switch через прямой вызов platform-хуков
	// Это проверит что вызовы не падают
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("platformActivateKillSwitch panicked: %v", r)
			}
		}()
		vpn.platformActivateKillSwitch()
	}()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("platformDeactivateKillSwitch panicked: %v", r)
			}
		}()
		vpn.platformDeactivateKillSwitch()
	}()

	t.Log("KillSwitch hook calls completed without panic")
}
