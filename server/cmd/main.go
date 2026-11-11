package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kirill-Katz/rc-lab/interal/connection"
)

func main() {
	host := flag.String("host", "127.0.0.1", "IPv4 bind address (use 0.0.0.0 for LAN access)")
	port := flag.Int("port", 8080, "TCP port (1–65535)")
	flag.Parse()
	if *port < 1 || *port > 65535 || flag.NArg() != 0 {
		log.Print("usage: coffee-server [-host IPv4] [-port 1..65535]")
		os.Exit(2)
	}
	server, err := connection.Listen(*host, *port)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Serve(ctx); err != nil {
		log.Fatal(err)
	}
	log.Print("server stopped")
}
