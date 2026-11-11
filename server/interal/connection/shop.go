package connection

import (
	"log"
	"math/rand/v2"
	"runtime"
	"sync"
	"time"
)

const maxStock = 1_000_000

var products = []string{"coffee", "tea", "croissant"}
var preparation = map[string]int{"coffee": 4, "tea": 3, "croissant": 2}

type order struct {
	id       uint64
	client   *Connection
	items    map[string]int
	seconds  int
	canceled chan struct{}
	active   bool // protected by Shop.mu, covers queued AND preparing orders
}

// Shop owns all shared business state. No blocking socket IO happens under mu.
// Queue insertion, reservation, acceptance enqueue and worker wakeup are atomic.
type Shop struct {
	mu        sync.Mutex
	available *sync.Cond
	stock     map[string]int
	reserved  map[string]int
	pending   map[*Connection]map[uint64]*order
	queue     []*order
	provider  *Connection
	nextID    uint64
	closing   bool
	workers   sync.WaitGroup
	timeUnit  time.Duration
}

func NewShop() *Shop {
	stock := make(map[string]int)
	for _, name := range products {
		stock[name] = rand.IntN(41) + 10
	}
	return newShop(stock, runtime.NumCPU(), time.Second)
}
func newShop(stock map[string]int, workers int, unit time.Duration) *Shop {
	s := &Shop{stock: make(map[string]int), reserved: make(map[string]int), pending: make(map[*Connection]map[uint64]*order), timeUnit: unit}
	for _, name := range products {
		s.stock[name] = stock[name]
	}
	s.available = sync.NewCond(&s.mu)
	for i := 0; i < workers; i++ {
		s.workers.Add(1)
		go s.worker(i + 1)
	}
	log.Printf("shop workers=%d initial_stock=%v", workers, s.stock)
	return s
}
func (s *Shop) snapshot() map[string]int {
	stock := make(map[string]int, len(s.stock))
	for name, qty := range s.stock {
		stock[name] = qty
	}
	return stock
}
func (s *Shop) authenticate(c *Connection, id string, role Role) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.Role != RoleUnknown {
		c.fail(id, "already_authenticated", "Role cannot be changed")
		return
	}
	if role != RoleClient && role != RoleProvider {
		c.fail(id, "invalid_role", "Role must be client or provider")
		return
	}
	if role == RoleProvider {
		if s.provider != nil && !s.provider.stopped() {
			c.fail(id, "provider_exists", "A provider is already connected")
			return
		}
		s.provider = c
	}
	c.Role = role
	c.send(map[string]any{"type": "auth_ok", "id": id, "role": role})
	log.Printf("endpoint=%s authenticated role=%s", c.endpoint, role)
	if role == RoleProvider {
		for _, name := range products {
			if s.stock[name] < 5 {
				s.alert(name)
			}
		}
	}
}
func (s *Shop) menu(c *Connection, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.send(map[string]any{"type": "menu", "id": id, "stock": s.snapshot(), "preparation_seconds": preparation})
}
func (s *Shop) order(c *Connection, id string, items map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || c.stopped() {
		return
	}
	for name, qty := range items {
		if qty > s.stock[name] {
			c.send(map[string]any{"type": "order_refused", "id": id, "stock": s.snapshot(), "msg": "Insufficient stock"})
			log.Printf("endpoint=%s order=refused items=%v stock=%v", c.endpoint, items, s.stock)
			return
		}
	}
	s.nextID++
	o := &order{id: s.nextID, client: c, items: items, canceled: make(chan struct{}), active: true}
	var low []string
	for name, qty := range items {
		before := s.stock[name]
		s.stock[name] -= qty
		s.reserved[name] += qty
		o.seconds += qty * preparation[name]
		if before >= 5 && s.stock[name] < 5 {
			low = append(low, name)
		}
	}
	if s.pending[c] == nil {
		s.pending[c] = make(map[uint64]*order)
	}
	s.pending[c][o.id] = o
	if !c.send(map[string]any{"type": "order_accepted", "id": id, "order_id": o.id, "preparation_seconds": o.seconds}) {
		s.cancel(o)
		return
	}
	s.queue = append(s.queue, o)
	log.Printf("endpoint=%s order=%d accepted items=%v preparation_seconds=%d", c.endpoint, o.id, items, o.seconds)
	for _, name := range low {
		s.alert(name)
	}
	s.available.Signal()
}
func (s *Shop) deliver(c *Connection, id string, items map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, qty := range items {
		if qty > maxStock-s.stock[name]-s.reserved[name] {
			c.fail(id, "stock_limit", "Delivery exceeds capacity including refundable reservations")
			return
		}
	}
	for name, qty := range items {
		s.stock[name] += qty
	}
	c.send(map[string]any{"type": "delivery_accepted", "id": id, "stock": s.snapshot()})
	log.Printf("endpoint=%s delivery=%v stock=%v", c.endpoint, items, s.stock)
}
func (s *Shop) alert(name string) {
	if s.provider != nil {
		s.provider.send(map[string]any{"type": "low_stock", "product": name, "quantity": s.stock[name]})
	}
}
func (s *Shop) forget(o *order) {
	o.active = false
	for name, qty := range o.items {
		s.reserved[name] -= qty
	}
	delete(s.pending[o.client], o.id)
	if len(s.pending[o.client]) == 0 {
		delete(s.pending, o.client)
	}
}
func (s *Shop) cancel(o *order) {
	if !o.active {
		return
	}
	for name, qty := range o.items {
		s.stock[name] += qty
	}
	close(o.canceled)
	s.forget(o)
	log.Printf("endpoint=%s order=%d canceled refunded=%v", o.client.endpoint, o.id, o.items)
}
func (s *Shop) disconnect(c *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider == c {
		s.provider = nil
	}
	for _, o := range s.pending[c] {
		s.cancel(o)
	}
	// Remove canceled queue entries immediately, without retaining dead clients.
	live := s.queue[:0]
	for _, o := range s.queue {
		if o.active {
			live = append(live, o)
		}
	}
	for i := len(live); i < len(s.queue); i++ {
		s.queue[i] = nil
	}
	s.queue = live
}
func (s *Shop) worker(number int) {
	defer s.workers.Done()
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closing {
			s.available.Wait()
		}
		if s.closing {
			s.mu.Unlock()
			return
		}
		o := s.queue[0]
		s.queue[0] = nil
		s.queue = s.queue[1:]
		log.Printf("endpoint=%s order=%d preparing worker=%d", o.client.endpoint, o.id, number)
		s.mu.Unlock()

		timer := time.NewTimer(time.Duration(o.seconds) * s.timeUnit)
		select {
		case <-timer.C:
		case <-o.canceled:
		}
		timer.Stop()

		s.mu.Lock()
		if o.active {
			if o.client.stopped() || !o.client.send(map[string]any{"type": "order_ready", "order_id": o.id}) {
				s.cancel(o)
			} else {
				s.forget(o)
				log.Printf("endpoint=%s order=%d ready worker=%d", o.client.endpoint, o.id, number)
			}
		}
		s.mu.Unlock()
	}
}
func (s *Shop) Close() {
	s.mu.Lock()
	s.closing = true
	for _, orders := range s.pending {
		for _, o := range orders {
			s.cancel(o)
		}
	}
	s.queue = nil
	s.available.Broadcast()
	s.mu.Unlock()
	s.workers.Wait()
}
