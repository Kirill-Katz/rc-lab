package connection

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type peer struct {
	t *testing.T
	net.Conn
	handlerDone chan struct{}
}

func testShop(t *testing.T, stock map[string]int, workers int, unit time.Duration) *Shop {
	t.Helper()
	s := newShop(stock, workers, unit)
	t.Cleanup(s.Close)
	return s
}
func connectPeer(t *testing.T, s *Shop) *peer {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(fds[1]), "test-peer")
	conn, err := net.FileConn(f)
	_ = f.Close()
	if err != nil {
		_ = unix.Close(fds[0])
		t.Fatal(err)
	}
	c := newConnection(fds[0], "test-peer", s)
	p := &peer{t: t, Conn: conn, handlerDone: make(chan struct{})}
	go func() { c.Handle(); close(p.handlerDone) }()
	t.Cleanup(func() { p.closeAndWait() })
	p.expect("auth_req")
	return p
}
func (p *peer) closeAndWait() {
	p.t.Helper()
	_ = p.Conn.Close()
	if p.handlerDone != nil {
		select {
		case <-p.handlerDone:
		case <-time.After(3 * time.Second):
			p.t.Error("handler did not stop")
		}
	}
}
func (p *peer) send(payload string) {
	p.t.Helper()
	_ = p.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := writeMessage(p.Conn, []byte(payload)); err != nil {
		p.t.Fatal(err)
	}
}
func (p *peer) expect(kind string) map[string]any {
	p.t.Helper()
	_ = p.SetReadDeadline(time.Now().Add(3 * time.Second))
	payload, err := readMessage(p.Conn)
	if err != nil {
		p.t.Fatalf("read %s: %v", kind, err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		p.t.Fatal(err)
	}
	if msg["type"] != kind {
		p.t.Fatalf("expected %s, got %s", kind, payload)
	}
	return msg
}
func (p *peer) auth(role string) {
	p.t.Helper()
	p.send(fmt.Sprintf(`{"type":"auth_res","id":"auth","role":%q}`, role))
	p.expect("auth_ok")
}
func (p *peer) errorCode(payload, code string) {
	p.t.Helper()
	p.send(payload)
	if msg := p.expect("error"); msg["code"] != code {
		p.t.Fatalf("expected error %s, got %v", code, msg)
	}
}
func stockOf(t *testing.T, msg map[string]any, name string) int {
	t.Helper()
	return int(msg["stock"].(map[string]any)[name].(float64))
}

func TestValidationAndRoles(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 10, "tea": 10, "croissant": 10}, 0, time.Second)
	p := connectPeer(t, s)
	p.errorCode(`{"type":"menu","id":"m"}`, "authentication_required")
	p.errorCode(`{"type":"auth_res","id":"a","role":"admin"}`, "invalid_role")
	p.errorCode(`{"type":"wat","id":"x"}`, "unknown_command")
	for _, payload := range []string{`no json`, `null`, `[]`, `{"type":"menu"}`, `{"type":"menu","id":3}`, `{"type":"menu","id":"m","extra":1}`, `{"type":"menu","id":"m"} {}`, string([]byte{0xff})} {
		p.errorCode(payload, "invalid_message")
	}
	p.auth("client")
	p.errorCode(`{"type":"auth_res","id":"a","role":"provider"}`, "already_authenticated")
	p.errorCode(`{"type":"menu","id":"m","items":{}}`, "invalid_message")
	p.errorCode(`{"type":"delivery","id":"d","items":{"coffee":1}}`, "forbidden")
	for _, items := range []string{`{}`, `null`, `[]`, `{"coffee":0}`, `{"coffee":-1}`, `{"coffee":1.5}`, `{"coffee":"1"}`, `{"coffee":null}`, `{"coffee":1000001}`, `{"sandwich":1}`} {
		p.errorCode(`{"type":"order","id":"o","items":`+items+`}`, "invalid_items")
	}
	p.errorCode(`{"type":"order","id":"o"}`, "invalid_items")
	p.send(`{"type":"order","id":"refused","items":{"coffee":11,"tea":1}}`)
	p.expect("order_refused")
	p.send(`{"type":"menu","id":"m"}`)
	msg := p.expect("menu")
	if stockOf(t, msg, "coffee") != 10 || stockOf(t, msg, "tea") != 10 {
		t.Fatal("invalid/refused input mutated stock", msg)
	}
}

func TestProviderAlertsAndDelivery(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 6, "tea": 10, "croissant": 10}, 0, time.Second)
	c := connectPeer(t, s)
	c.auth("client")
	c.send(`{"type":"order","id":"1","items":{"coffee":3}}`)
	c.expect("order_accepted")
	p := connectPeer(t, s)
	p.auth("provider")
	if msg := p.expect("low_stock"); msg["product"] != "coffee" || msg["quantity"] != float64(3) {
		t.Fatal(msg)
	}
	p2 := connectPeer(t, s)
	p2.errorCode(`{"type":"auth_res","id":"a","role":"provider"}`, "provider_exists")
	p.errorCode(`{"type":"order","id":"o","items":{"tea":1}}`, "forbidden")
	p.send(`{"type":"delivery","id":"d","items":{"coffee":3,"tea":2}}`)
	if msg := p.expect("delivery_accepted"); stockOf(t, msg, "coffee") != 6 || stockOf(t, msg, "tea") != 12 {
		t.Fatal(msg)
	}
	c.send(`{"type":"order","id":"2","items":{"coffee":2}}`)
	c.expect("order_accepted")
	if msg := p.expect("low_stock"); msg["quantity"] != float64(4) {
		t.Fatal(msg)
	}
	c.send(`{"type":"order","id":"3","items":{"coffee":1}}`)
	c.expect("order_accepted")
	p.send(`{"type":"menu","id":"barrier"}`)
	p.expect("menu") // no duplicate alert while already below five
	p.closeAndWait()
	p2.auth("provider")
	if msg := p2.expect("low_stock"); msg["quantity"] != float64(3) {
		t.Fatal(msg)
	}
	c.closeAndWait() // refunds rearm the threshold
	p2.send(`{"type":"menu","id":"m"}`)
	if msg := p2.expect("menu"); stockOf(t, msg, "coffee") != 9 {
		t.Fatal(msg)
	}
	c2 := connectPeer(t, s)
	c2.auth("client")
	c2.send(`{"type":"order","id":"4","items":{"coffee":5}}`)
	c2.expect("order_accepted")
	p2.expect("low_stock")
}

func TestDeliveryCapacityIncludesReservations(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": maxStock - 1, "tea": 10, "croissant": 10}, 0, time.Second)
	c := connectPeer(t, s)
	c.auth("client")
	p := connectPeer(t, s)
	p.auth("provider")
	c.send(`{"type":"order","id":"o","items":{"coffee":1}}`)
	c.expect("order_accepted")
	p.errorCode(`{"type":"delivery","id":"d","items":{"coffee":2,"tea":1}}`, "stock_limit")
	p.send(`{"type":"menu","id":"m"}`)
	if msg := p.expect("menu"); stockOf(t, msg, "tea") != 10 || stockOf(t, msg, "coffee") != maxStock-2 {
		t.Fatal("partial delivery", msg)
	}
	p.send(`{"type":"delivery","id":"d2","items":{"coffee":1}}`)
	p.expect("delivery_accepted")
	c.closeAndWait()
	p.send(`{"type":"menu","id":"m2"}`)
	if msg := p.expect("menu"); stockOf(t, msg, "coffee") != maxStock {
		t.Fatal("refund overflow", msg)
	}
}

func TestOrdersReadyAndFIFO(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 20, "tea": 20, "croissant": 20}, 1, 20*time.Millisecond)
	p := connectPeer(t, s)
	p.auth("client")
	p.send(`{"type":"order","id":"first","items":{"coffee":1,"tea":1,"croissant":2}}`)
	first := p.expect("order_accepted")
	if first["preparation_seconds"] != float64(11) {
		t.Fatal(first)
	}
	p.send(`{"type":"order","id":"second","items":{"croissant":1}}`)
	second := p.expect("order_accepted")
	p.send(`{"type":"menu","id":"during"}`)
	if msg := p.expect("menu"); stockOf(t, msg, "coffee") != 19 || stockOf(t, msg, "croissant") != 17 {
		t.Fatal(msg)
	}
	if msg := p.expect("order_ready"); msg["order_id"] != first["order_id"] {
		t.Fatal("FIFO violated", msg)
	}
	if msg := p.expect("order_ready"); msg["order_id"] != second["order_id"] {
		t.Fatal("FIFO violated", msg)
	}
	p.closeAndWait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stock["coffee"] != 19 || s.stock["croissant"] != 17 || len(s.pending) != 0 {
		t.Fatal("ready order refunded", s.stock)
	}
}

func TestDisconnectCancelsQueuedAndPreparing(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 10, "tea": 10, "croissant": 10}, 1, time.Second)
	p := connectPeer(t, s)
	p.auth("client")
	p.send(`{"type":"order","id":"active","items":{"coffee":1}}`)
	p.expect("order_accepted")
	p.send(`{"type":"order","id":"queued","items":{"tea":1}}`)
	p.expect("order_accepted")
	await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.queue) == 1 })
	p.closeAndWait()
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stock["coffee"] != 10 || s.stock["tea"] != 10 || len(s.pending) != 0 || len(s.queue) != 0 {
			t.Fatal("incomplete refund", s.stock)
		}
	}()
	// If the first worker kept its four-second preparation wait, this order
	// could not become ready within the three-second read deadline.
	next := connectPeer(t, s)
	next.auth("client")
	next.send(`{"type":"order","id":"next","items":{"croissant":1}}`)
	next.expect("order_accepted")
	next.expect("order_ready")
}

func TestConcurrentLastProduct(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 10, "tea": 10, "croissant": 1}, 0, time.Second)
	const count = 64
	clients := make([]*Connection, count)
	var wg sync.WaitGroup
	for i := range clients {
		c := newConnection(-1, fmt.Sprintf("race-%d", i), s)
		clients[i] = c
		wg.Add(1)
		go func() { defer wg.Done(); s.order(c, "o", map[string]int{"croissant": 1}) }()
	}
	wg.Wait()
	accepted := 0
	for _, c := range clients {
		p := <-c.out
		var msg map[string]any
		if err := json.Unmarshal(p.payload, &msg); err != nil {
			t.Fatal(err)
		}
		if msg["type"] == "order_accepted" {
			accepted++
		} else if msg["type"] != "order_refused" {
			t.Fatal(msg)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d orders for one croissant", accepted)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stock["croissant"] != 0 || s.reserved["croissant"] != 1 {
		t.Fatal("stock race")
	}
}

func TestMalformedFrameDisconnectsOnlyItsPeer(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 10, "tea": 10, "croissant": 10}, 0, time.Second)
	for _, length := range []uint32{0, MAX_MSG_LEN + 1} {
		bad := connectPeer(t, s)
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], length)
		if _, err := bad.Write(header[:]); err != nil {
			t.Fatal(err)
		}
		if msg := bad.expect("error"); msg["code"] != "invalid_frame" {
			t.Fatal(msg)
		}
		_ = bad.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := readMessage(bad.Conn); err != io.EOF {
			t.Fatalf("expected disconnect, got %v", err)
		}
	}
	good := connectPeer(t, s)
	good.auth("client")
	good.send(`{"type":"menu","id":"m"}`)
	good.expect("menu")
}

func TestSlowPeerDoesNotBlockShop(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 10, "tea": 10, "croissant": 10}, 0, time.Second)
	slow := newConnection(-1, "slow-peer", s)
	s.order(slow, "o", map[string]int{"coffee": 1})
	for i := 0; i < 64; i++ {
		slow.send(map[string]any{"type": "menu"})
	}
	if !slow.stopped() {
		t.Fatal("full queue did not disconnect slow peer")
	}
	s.disconnect(slow)
	good := connectPeer(t, s)
	good.auth("client")
	good.send(`{"type":"menu","id":"m"}`)
	if msg := good.expect("menu"); stockOf(t, msg, "coffee") != 10 {
		t.Fatal("slow peer reservation was not refunded", msg)
	}
}

func TestActualTCPAndShutdown(t *testing.T) {
	server, err := Listen("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server shutdown timed out")
		}
	})
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", server.Port), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	p := &peer{t: t, Conn: conn}
	t.Cleanup(func() { _ = conn.Close() })
	p.expect("auth_req")
	// Fragmented authentication, then coalesced menu requests over real TCP.
	var frames bytes.Buffer
	_ = writeMessage(&frames, []byte(`{"type":"auth_res","id":"a","role":"client"}`))
	for _, b := range frames.Bytes() {
		if _, err := conn.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	p.expect("auth_ok")
	frames.Reset()
	_ = writeMessage(&frames, []byte(`{"type":"menu","id":"1"}`))
	_ = writeMessage(&frames, []byte(`{"type":"menu","id":"2"}`))
	if _, err := conn.Write(frames.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2"} {
		msg := p.expect("menu")
		if msg["id"] != id {
			t.Fatal("correlation lost", msg)
		}
		for _, name := range products {
			if qty := stockOf(t, msg, name); qty < 10 || qty > 50 {
				t.Fatal("invalid startup stock", msg)
			}
		}
	}
	// Leave a reader blocked in a partial frame; cancellation must still finish.
	if _, err := conn.Write([]byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var b [1]byte
	if _, err := conn.Read(b[:]); err == nil {
		t.Fatal("connection remained open after shutdown")
	}
}
