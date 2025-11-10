package main

import (
	"flag"
	"log"
	"net"
	"net/rpc"

	"uk.ac.bris.cs/gameoflife/gol"
)

func main() {
	addr := flag.String("listen", ":6000", "Address for the Gol engine RPC server")
	flag.Parse()

	if err := rpc.RegisterName(gol.EngineServiceName, gol.NewEngineService()); err != nil {
		log.Fatalf("failed to register engine RPC service: %v", err)
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *addr, err)
	}
	log.Printf("[Engine] Listening on %s", *addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[Engine] Accept error: %v", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
