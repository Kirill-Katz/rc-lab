package connection

import (
	"golang.org/x/sys/unix"
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
	FD int
	Role Role
}

func (c *Connection) Handle() {
	defer unix.Close(c.FD)

	msg, err := readMessage(c.FD)
	if err != nil {
		log.Printf("Failed to read the message: %v", err)
		return
	}

	strMsg := string(msg)
	log.Printf("%s", strMsg)
}
