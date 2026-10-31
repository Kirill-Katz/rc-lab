package connection

import (
	"encoding/json"
	"sync"
	"fmt"
	"golang.org/x/sys/unix"
)

type Message struct {
	Type string `json:"type"`
	Msg string `json:"msg"`
}

type ClientManager struct {
	mu sync.Mutex
	Clients map[int]*Connection
	Providers map[int]*Connection
}

func (m *ClientManager) Connect(fd int) error {
	conn := &Connection {
		Socket: Socket{fd},
		Role: RoleUnknown,
	}

	defer unix.Close(fd)

	request, err := json.Marshal(Message{
		Type: "auth_req",
		Msg: "Please specify the client type: Client or Provider",
	})

	if err != nil {
		return err
	}

	if err := writeMessage(&conn.Socket, request); err != nil {
		return err
	}

	payload, err := readMessage(&conn.Socket)
	if err != nil {
		return err
	}

	var response Message
	if err := json.Unmarshal(payload[4:], &response); err != nil {
		return err
	}

	if response.Type != "auth_res" {
		return fmt.Errorf("expected auth_res, got %s", response.Type)
	}

	switch response.Msg {
	case "client":
		conn.Role = RoleClient
	case "provider":
		conn.Role = RoleProvider
	default:
		return fmt.Errorf("unknown role: %s", response.Msg)
	}

	m.mu.Lock()

    switch conn.Role {
    case RoleClient:
        m.Clients[fd] = conn
    case RoleProvider:
        m.Providers[fd] = conn
    }

    m.mu.Unlock()
	defer m.Disconnect(conn)

	conn.Handle()

	return nil
}

func (m *ClientManager) Disconnect(conn *Connection) {
	m.mu.Lock()
	defer m.mu.Unlock()

    switch conn.Role {
    case RoleClient:
        delete(m.Clients, conn.Socket.FD)
    case RoleProvider:
        delete(m.Providers, conn.Socket.FD)
    }
}
