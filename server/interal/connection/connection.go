package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type Role string

const (
	RoleUnknown  Role = ""
	RoleClient   Role = "client"
	RoleProvider Role = "provider"
)

type packet struct {
	payload []byte
	sent    chan struct{}
}

type Connection struct {
	Socket     Socket
	Role       Role // read/written only by handler, and under the shop lock
	endpoint   string
	shop       *Shop
	out        chan packet
	done       chan struct{}
	writerDone chan struct{}
	stopOnce   sync.Once
}

func newConnection(fd int, endpoint string, s *Shop) *Connection {
	return &Connection{Socket: Socket{FD: fd}, endpoint: endpoint, shop: s,
		out: make(chan packet, 64), done: make(chan struct{}), writerDone: make(chan struct{})}
}

// stop never takes the shop lock or closes the descriptor. The handler owns
// close, after the writer exits, so an old goroutine cannot use a recycled FD.
func (c *Connection) stop() {
	c.stopOnce.Do(func() { close(c.done); _ = unix.Shutdown(c.Socket.FD, unix.SHUT_RDWR) })
}
func (c *Connection) stopped() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
func (c *Connection) enqueue(message any, sent chan struct{}) bool {
	if c.stopped() {
		return false
	}
	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("endpoint=%s error=encode: %v", c.endpoint, err)
		c.stop()
		return false
	}
	select {
	case c.out <- packet{payload: payload, sent: sent}:
		return true
	default:
		log.Printf("endpoint=%s error=outgoing_queue_full", c.endpoint)
		c.stop()
		return false
	}
}
func (c *Connection) send(message any) bool { return c.enqueue(message, nil) }
func (c *Connection) fail(id, code, msg string) {
	// Never let echoed invalid input turn a valid-sized request into an
	// oversized error response (or an arbitrarily large log entry).
	if len(msg) > 256 {
		msg = msg[:256]
		for !utf8.ValidString(msg) {
			msg = msg[:len(msg)-1]
		}
		msg += "..."
	}
	log.Printf("endpoint=%s error=%s detail=%q", c.endpoint, code, msg)
	response := map[string]any{"type": "error", "code": code, "msg": msg}
	if id != "" {
		response["id"] = id
	}
	c.send(response)
}
func (c *Connection) writeLoop() {
	defer close(c.writerDone)
	for {
		select {
		case <-c.done:
			return
		case p := <-c.out:
			if err := writeMessage(&c.Socket, p.payload); err != nil {
				log.Printf("endpoint=%s error=write: %v", c.endpoint, err)
				c.stop()
				return
			}
			if p.sent != nil {
				close(p.sent)
			}
		}
	}
}

func (c *Connection) Handle() {
	go c.writeLoop()
	defer func() {
		c.stop()
		c.shop.disconnect(c)
		<-c.writerDone
		c.Socket.Close()
		log.Printf("endpoint=%s disconnected", c.endpoint)
	}()
	c.send(map[string]any{"type": "auth_req", "msg": "Identify with auth_res: client or provider"})
	for {
		payload, err := readMessage(&c.Socket)
		if err != nil {
			if errors.Is(err, ErrFrameLength) {
				log.Printf("endpoint=%s error=invalid_frame: %v", c.endpoint, err)
				sent := make(chan struct{})
				if c.enqueue(map[string]any{"type": "error", "code": "invalid_frame", "msg": err.Error()}, sent) {
					select {
					case <-sent:
					case <-c.done:
					}
				}
			} else if !errors.Is(err, io.EOF) {
				log.Printf("endpoint=%s error=read: %v", c.endpoint, err)
			}
			return
		}
		c.handleMessage(payload)
		if c.stopped() {
			return
		}
	}
}

type request struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Role  Role            `json:"role"`
	Items json.RawMessage `json:"items"`
}

func (c *Connection) handleMessage(payload []byte) {
	var req request
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(payload) || decoder.Decode(&req) != nil || req.Type == "" || len(req.ID) == 0 || len(req.ID) > 64 {
		c.fail("", "invalid_message", "Expected a UTF-8 JSON request with type and a nonempty id (up to 64 bytes)")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		c.fail(req.ID, "invalid_message", "Expected exactly one JSON object")
		return
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(payload, &fields)
	allowed := map[string]bool{"type": true, "id": true}
	switch req.Type {
	case "auth_res":
		allowed["role"] = true
	case "order", "delivery":
		allowed["items"] = true
	case "menu":
	default:
		c.fail(req.ID, "unknown_command", "Unknown command: "+req.Type)
		return
	}
	for field := range fields {
		if !allowed[field] {
			c.fail(req.ID, "invalid_message", "Unexpected field: "+field)
			return
		}
	}
	if req.Type == "auth_res" {
		c.shop.authenticate(c, req.ID, req.Role)
		return
	}
	if c.Role == RoleUnknown {
		c.fail(req.ID, "authentication_required", "Send auth_res first")
		return
	}
	switch req.Type {
	case "menu":
		c.shop.menu(c, req.ID)
	case "order", "delivery":
		if (req.Type == "order" && c.Role != RoleClient) || (req.Type == "delivery" && c.Role != RoleProvider) {
			c.fail(req.ID, "forbidden", "Command is not allowed for this role")
			return
		}
		var items map[string]int
		if err := json.Unmarshal(req.Items, &items); err != nil || len(items) == 0 {
			c.fail(req.ID, "invalid_items", "Items must be a nonempty object of integer quantities")
			return
		}
		for name, qty := range items {
			if _, ok := preparation[name]; !ok {
				c.fail(req.ID, "invalid_items", "Unknown product: "+name)
				return
			}
			if qty <= 0 || qty > maxStock {
				c.fail(req.ID, "invalid_items", "Quantities must be integers from 1 to 1000000")
				return
			}
		}
		if req.Type == "order" {
			c.shop.order(c, req.ID, items)
		} else {
			c.shop.deliver(c, req.ID, items)
		}
	}
}
