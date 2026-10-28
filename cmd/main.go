package main

import (
	"fmt"
	"log"
	"math/rand"

	"github.com/Kirill-Katz/rc-lab/interal/connection"
	"golang.org/x/sys/unix"
)

type Stock struct {
	Coffee int
	Tea int
	Croissant int
}

func NewStock() *Stock {
	return &Stock{
		Coffee: rand.Intn(41) + 10,
		Tea: rand.Intn(41) + 10,
		Croissant: rand.Intn(41) + 10,
	};
}

func main() {
	fmt.Println("Hello world!")

	// stock := NewStock()

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		log.Fatal(err)
	}
	defer unix.Close(fd)

	err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	if err != nil {
		log.Fatal(err)
	}

	addr := &unix.SockaddrInet4{
		Port: 8080,
		Addr: [4]byte{127, 0, 0, 1},
	}

	if err := unix.Bind(fd, addr); err != nil {
		log.Fatal(err)
	}

	if err := unix.Listen(fd, 128); err != nil {
		log.Fatal(err)
	}

	log.Println("Listening on 127.0.0.0:8080")

	connectionManager := connection.ClientManager{}

	for {
		clientFD, clientAddr, err := unix.Accept(fd)
		if err == unix.EINTR {
			continue
		}

		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		if err := unix.SetsockoptInt(clientFD, unix.SOL_SOCKET, unix.SO_NOSIGPIPE, 1); err != nil {
			log.Printf("Socket option error: %v", err)
			unix.Close(clientFD)
			continue
		}

		log.Printf("New connection: %v", clientAddr)

		go connectionManager.Connect(fd)
	}

}
