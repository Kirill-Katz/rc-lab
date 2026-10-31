package connection

import (
	"log"
)

type Role uint8

const (
	RoleUnknown Role = iota
	RoleClient
	RoleProvider
)

const MAX_MSG_LEN = 2 * 1024 * 1024 // 2 MB

type Connection struct {
	Socket Socket
	Role Role
}

func (c *Connection) Handle() {
	defer c.Socket.Close()

	msg, err := readMessage(&c.Socket)
	if err != nil {
		log.Printf("Failed to read the message: %v", err)
		return
	}

	strMsg := string(msg)
	log.Printf("%s", strMsg)
}
