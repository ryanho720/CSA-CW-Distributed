package gol

import (
	"fmt"
	"net/rpc"
	"os"
	"testing"
	"time"
)

// benchmark the remote engine's Process RPC
// requires ENGINE_ADDR to be set (e.g., ENGINE_ADDR=<ip>) and the engine running
func BenchmarkRemoteProcess(b *testing.B) {
	addr := os.Getenv("ENGINE_ADDR")
	if addr == "" {
		b.Skip("ENGINE_ADDR not set")
	}

	client, err := rpc.Dial("tcp", addr)
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	defer client.Close()

	width, height := 64, 64
	world := makeWorld(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if (x+y)%2 == 0 {
				world[y][x] = 255
			}
		}
	}

	req := EngineRequest{
		Params: Params{
			Turns:       10,
			Threads:     4,
			ImageWidth:  width,
			ImageHeight: height,
		},
		World: worldToBytes(world),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req.SessionID = fmt.Sprintf("bench-%d-%d", time.Now().UnixNano(), i)
		var resp EngineResponse
		if err := client.Call(EngineServiceName+".Process", req, &resp); err != nil {
			b.Fatalf("Process: %v", err)
		}
	}
}
