package connection

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func await(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestWorkerPoolQueuesMoreClientsThanWorkers(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 20, "tea": 20, "croissant": 20}, 2, time.Hour)
	clients := []*peer{connectPeer(t, s), connectPeer(t, s), connectPeer(t, s)}
	for _, c := range clients {
		c.auth("client")
		c.send(`{"type":"order","id":"o","items":{"coffee":1}}`)
		c.expect("order_accepted")
	}
	await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.queue) == 1 })
	clients[0].closeAndWait()
	// Cancellation interrupts the hour-long wait and frees a worker immediately.
	await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.queue) == 0 })
	for _, c := range clients {
		c.closeAndWait()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stock["coffee"] != 20 || s.reserved["coffee"] != 0 || len(s.pending) != 0 {
		t.Fatal("canceled work leaked reservations", s.stock)
	}
}

func TestConcurrentDeliveriesAndOrders(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 100, "tea": 100, "croissant": 100}, 0, time.Second)
	c := newConnection(-1, "ordering", s)
	p := newConnection(-1, "delivering", s)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.order(c, "o", map[string]int{"coffee": 1, "tea": 1}) }()
		go func() { defer wg.Done(); s.deliver(p, "d", map[string]int{"coffee": 1, "tea": 1}) }()
	}
	wg.Wait()
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, name := range []string{"coffee", "tea"} {
			if s.stock[name] != 100 || s.reserved[name] != 20 {
				t.Fatal("lost stock update", s.stock, s.reserved)
			}
		}
	}()
	s.disconnect(c)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{"coffee", "tea"} {
		if s.stock[name] != 120 || s.reserved[name] != 0 {
			t.Fatal("lost refund", s.stock, s.reserved)
		}
	}
}

func TestLargeInvalidInputGetsRecoverableError(t *testing.T) {
	s := testShop(t, map[string]int{"coffee": 10, "tea": 10, "croissant": 10}, 0, time.Second)
	p := connectPeer(t, s)
	p.errorCode(`{"type":"`+strings.Repeat("x", MAX_MSG_LEN-100)+`","id":"x"}`, "unknown_command")
	p.auth("client")
	p.errorCode(`{"type":"order","id":"x","items":{"`+strings.Repeat("x", MAX_MSG_LEN-100)+`":1}}`, "invalid_items")
	p.errorCode(`{"type":"menu","id":"`+strings.Repeat("x", 65)+`"}`, "invalid_message")
	p.send(`{"type":"menu","id":"still_alive"}`)
	p.expect("menu")
}
