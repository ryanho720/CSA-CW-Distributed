package gol

import (
	"fmt"
	"testing"
)

// Usage:
//   go test -bench RunTurnThreads -benchmem ./gol > runturn.out
//   benchstat -format csv runturn.out > runturn.csv
//   python gol/plot.py
// This mirrors the workflow used in the parallel implementation for plotting.

// Benchmark runTurn throughput across thread counts and sizes, mirroring the
// parallel implementation comparison.
func BenchmarkRunTurnThreads(b *testing.B) {
	cases := []struct {
		name    string
		width   int
		height  int
		threads []int
	}{
		{"64x64", 64, 64, []int{1, 2, 4, 8}},
		{"256x256", 256, 256, []int{1, 2, 4, 8, 16}},
	}

	for _, c := range cases {
		// build a stable starting world with a checkerboard pattern
		base := makeWorld(c.width, c.height)
		for y := 0; y < c.height; y++ {
			for x := 0; x < c.width; x++ {
				if (x+y)%2 == 0 {
					base[y][x] = 255
				}
			}
		}

		for _, threads := range c.threads {
			b.Run(fmt.Sprintf("%s_threads_%d", c.name, threads), func(b *testing.B) {
				world := cloneWorld(base)
				next := makeWorld(c.width, c.height)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					runTurn(world, next, c.width, c.height, threads, false)
					world, next = next, world
				}
			})
		}
	}
}

func cloneWorld(src [][]byte) [][]byte {
	height := len(src)
	if height == 0 {
		return nil
	}
	width := len(src[0])
	dst := makeWorld(width, height)
	for y := 0; y < height; y++ {
		copy(dst[y], src[y])
	}
	return dst
}
