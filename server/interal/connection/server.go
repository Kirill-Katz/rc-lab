package connection

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Server uses socket/bind/listen/accept/read/send/shutdown/close directly.
// A nonblocking listening descriptor lets cancellation interrupt the accept loop.
// Accepted sockets are blocking, with one reader and one serialized writer each.
type Server struct {
	fd       int
	Port     int
	host     string
	shop     *Shop
	mu       sync.Mutex
	clients  map[*Connection]struct{}
	handlers sync.WaitGroup
}

func Listen(host string, port int) (*Server, error) {
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || port < 0 || port > 65535 {
		return nil, fmt.Errorf("expected an IPv4 address and port from 0 to 65535")
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	ok := false
	defer func() {
		if !ok {
			_ = unix.Close(fd)
		}
	}()
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return nil, err
	}
	if err = unix.Bind(fd, &unix.SockaddrInet4{Port: port, Addr: ip.As4()}); err != nil {
		return nil, err
	}
	if err = unix.Listen(fd, 128); err != nil {
		return nil, err
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		return nil, err
	}
	ok = true
	return &Server{fd: fd, Port: bound.(*unix.SockaddrInet4).Port, host: host, shop: NewShop(), clients: make(map[*Connection]struct{})}, nil
}

// Serve must be called exactly once, and owns shutdown of the listening socket,
// accepted sockets and workers. Cancellation waits for all handlers to exit.
func (s *Server) Serve(ctx context.Context) error {
	defer func() {
		_ = unix.Close(s.fd)
		s.mu.Lock()
		for c := range s.clients {
			c.stop()
		}
		s.mu.Unlock()
		s.handlers.Wait()
		s.shop.Close()
	}()
	log.Printf("listening on %s:%d", s.host, s.Port)
	for ctx.Err() == nil {
		fds := []unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 200)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("poll: %w", err)
		}
		if n == 0 {
			continue
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return fmt.Errorf("listening socket failed: %d", fds[0].Revents)
		}
		for ctx.Err() == nil {
			fd, addr, err := unix.Accept(s.fd)
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			if err != nil {
				log.Printf("endpoint=listener error=accept: %v", err)
				if err == unix.EBADF || err == unix.EINVAL {
					return fmt.Errorf("accept: %w", err)
				}
				// A reset in the accept queue or temporary descriptor/memory
				// exhaustion must not take down existing clients or busy-spin.
				select {
				case <-ctx.Done():
				case <-time.After(100 * time.Millisecond):
				}
				break
			}
			unix.CloseOnExec(fd)
			endpoint := formatEndpoint(addr)
			log.Printf("endpoint=%s connected", endpoint)
			err = unix.SetNonblock(fd, false)
			if err == nil {
				err = configureSocket(fd)
			}
			if err != nil {
				log.Printf("endpoint=%s error=socket_configuration: %v", endpoint, err)
				_ = unix.Close(fd)
				continue
			}
			c := newConnection(fd, endpoint, s.shop)
			s.mu.Lock()
			s.clients[c] = struct{}{}
			s.mu.Unlock()
			s.handlers.Add(1)
			go func() {
				defer s.handlers.Done()
				c.Handle()
				s.mu.Lock()
				delete(s.clients, c)
				s.mu.Unlock()
			}()
		}
	}
	return nil
}
func formatEndpoint(addr unix.Sockaddr) string {
	if a, ok := addr.(*unix.SockaddrInet4); ok {
		return fmt.Sprintf("%s:%d", netip.AddrFrom4(a.Addr), a.Port)
	}
	return fmt.Sprintf("%v", addr)
}
