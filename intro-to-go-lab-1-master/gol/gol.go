package main

func calculateNextState(p golParams, world [][]byte) [][]byte {
	worldc := make([][]byte, len(world))
	for i := range world {
		worldc[i] = make([]byte, len(world[i]))
	}
	for y := 0; y < len(world); y++ {
		for x := 0; x < len(world[y]); x++ {
			xp := (x + 1) % p.imageWidth
			xn := x - 1
			yn := y - 1
			yp := (y + 1) % p.imageHeight
			if xn < 0 {
				xn = p.imageWidth + xn
			}
			if yn < 0 {
				yn = p.imageHeight + yn
			}
			sum := int(world[y][xp]) + int(world[y][xn]) + int(world[yp][xp]) + int(world[yp][xn]) + int(world[yp][x]) + int(world[yn][xp]) + int(world[yn][xn]) + int(world[yn][x])
			if sum < 510 && world[y][x] == 255 {
				worldc[y][x] = 0
			} else if sum > 765 && world[y][x] == 255 {
				worldc[y][x] = 0
			} else if sum == 765 && world[y][x] == 0 {
				worldc[y][x] = 255
			} else {
				worldc[y][x] = world[y][x]
			}
		}
	}
	copy(world, worldc)
	return world
}

func calculateAliveCells(p golParams, world [][]byte) []cell {
	AliveList := make([]cell, 0)
	var AliveCell cell
	for y := 0; y < len(world); y++ {
		for x := 0; x < len(world[y]); x++ {
			if world[y][x] == 255 {
				AliveCell.x = x
				AliveCell.y = y
				AliveList = append(AliveList, AliveCell)
			}
		}
	}
	return AliveList
}
