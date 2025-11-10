package gol

import (
	"fmt"

	"uk.ac.bris.cs/gameoflife/util"
)

type workerResult struct {
	changes []util.Cell
	alive   int
}

func makeWorld(width, height int) [][]byte {
	world := make([][]byte, height)
	for y := range world {
		world[y] = make([]byte, width)
	}
	return world
}

func worldToBytes(world [][]byte) []byte {
	height := len(world)
	if height == 0 {
		return nil
	}
	width := len(world[0])
	data := make([]byte, width*height)
	idx := 0
	for y := 0; y < height; y++ {
		copy(data[idx:idx+width], world[y])
		idx += width
	}
	return data
}

func bytesToWorld(data []byte, width, height int) ([][]byte, error) {
	expected := width * height
	if len(data) != expected {
		return nil, fmt.Errorf("invalid world size: expected %d bytes, got %d", expected, len(data))
	}
	world := makeWorld(width, height)
	idx := 0
	for y := 0; y < height; y++ {
		copy(world[y], data[idx:idx+width])
		idx += width
	}
	return world, nil
}

func worldToAliveCells(world [][]byte) []util.Cell {
	height := len(world)
	if height == 0 {
		return nil
	}
	width := len(world[0])
	alive := make([]util.Cell, 0)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if world[y][x] == 255 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}
	return alive
}

func runTurn(world, next [][]byte, width, height, threads int, collectChanges bool) ([]util.Cell, int) {
	workerCount := threads
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > height {
		workerCount = height
	}

	results := make(chan workerResult, workerCount)

	rowsPerWorker := height / workerCount
	remainder := height % workerCount
	startY := 0
	for w := 0; w < workerCount; w++ {
		endY := startY + rowsPerWorker
		if w < remainder {
			endY++
		}
		sy, ey := startY, endY
		go func() {
			results <- processStrip(world, next, width, height, sy, ey, collectChanges)
		}()
		startY = endY
	}

	totalAlive := 0
	var changes []util.Cell
	for i := 0; i < workerCount; i++ {
		res := <-results
		totalAlive += res.alive
		if collectChanges && len(res.changes) > 0 {
			changes = append(changes, res.changes...)
		}
	}
	return changes, totalAlive
}

func processStrip(world, next [][]byte, width, height, startY, endY int, collectChanges bool) workerResult {
	var changes []util.Cell
	if collectChanges {
		changes = make([]util.Cell, 0)
	}
	alive := 0
	for y := startY; y < endY; y++ {
		for x := 0; x < width; x++ {
			xp := (x + 1) % width
			xn := x - 1
			yn := y - 1
			yp := (y + 1) % height
			if xn < 0 {
				xn = width + xn
			}
			if yn < 0 {
				yn = height + yn
			}
			sum := int(world[y][xp]) +
				int(world[y][xn]) +
				int(world[yp][xp]) +
				int(world[yp][xn]) +
				int(world[yp][x]) +
				int(world[yn][xp]) +
				int(world[yn][xn]) +
				int(world[yn][x])
			current := world[y][x]
			var nextVal byte
			switch {
			case sum < 510 && current == 255:
				nextVal = 0
			case sum > 765 && current == 255:
				nextVal = 0
			case sum == 765 && current == 0:
				nextVal = 255
			default:
				nextVal = current
			}
			next[y][x] = nextVal
			if nextVal == 255 {
				alive++
			}
			if collectChanges && nextVal != world[y][x] {
				changes = append(changes, util.Cell{X: x, Y: y})
			}
		}
	}
	return workerResult{changes: changes, alive: alive}
}

func countNeighbours(world [][]byte, width, height, x, y int) int {
	xn := x - 1
	if xn < 0 {
		xn = width - 1
	}
	xp := x + 1
	if xp == width {
		xp = 0
	}
	yn := y - 1
	if yn < 0 {
		yn = height - 1
	}
	yp := y + 1
	if yp == height {
		yp = 0
	}

	isAlive := func(v byte) int {
		if v == 255 {
			return 1
		}
		return 0
	}

	return isAlive(world[y][xn]) +
		isAlive(world[y][xp]) +
		isAlive(world[yn][xn]) +
		isAlive(world[yn][x]) +
		isAlive(world[yn][xp]) +
		isAlive(world[yp][xn]) +
		isAlive(world[yp][x]) +
		isAlive(world[yp][xp])
}
