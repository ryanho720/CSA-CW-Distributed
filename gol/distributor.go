package gol

import (
	"fmt"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan int32 // unicode character
}

// coordination of io/ui goroutines & work distribution
func distributor(p Params, c distributorChannels) {
	// calls EngineAddr if not empty
	// otherwise stays local
	if p.EngineAddr != "" {
		runRemoteDistributor(p, c)
		return
	}

	width, height := p.ImageWidth, p.ImageHeight

	// world holds the current state
	// nextWorld is the scratch buffer workers write into
	world := makeWorld(width, height)
	nextWorld := makeWorld(width, height)

	turn := 0

	initialAlive := make([]util.Cell, 0)
	currentAlive := 0

	if c.ioCommand != nil && c.ioFilename != nil && c.ioInput != nil {
		// tells the io goroutine to read the starting board into the world
		filename := fmt.Sprintf("%vx%v", width, height)
		c.ioCommand <- ioInput
		c.ioFilename <- filename
		// reads initial board from io and records which cells start alive
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				val := <-c.ioInput
				world[y][x] = val
				if val == 255 {
					initialAlive = append(initialAlive, util.Cell{X: x, Y: y})
				}
			}
		}
		// counts how many cells are alive in the starting board and reports them
		currentAlive = len(initialAlive)
		if len(initialAlive) > 0 {
			c.events <- CellsFlipped{CompletedTurns: turn, Cells: append([]util.Cell(nil), initialAlive...)}
		}
	} else {
		currentAlive = 0
	}

	c.events <- StateChange{turn, Executing}

	keyPresses := c.keyPresses
	paused := false
	quit := false

	// periodic ticker to emit AliveCellsCount events while running/paused
	aliveTicker := time.NewTicker(2 * time.Second)
	defer aliveTicker.Stop()

	// initialisation
	lastOutputTurn := -1

	// sends grid to goroutine so it could be written out as an image
	outputWorld := func() {
		if c.ioCommand == nil || c.ioFilename == nil || c.ioOutput == nil || c.ioIdle == nil {
			return
		}
		filename := fmt.Sprintf("%vx%vx%v", width, height, turn)
		c.ioCommand <- ioOutput
		c.ioFilename <- filename
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				c.ioOutput <- world[y][x]
			}
		}
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle
		c.events <- ImageOutputComplete{CompletedTurns: turn, Filename: filename}
		lastOutputTurn = turn
	}

	// handleKey changes state (pause/save/quit) based on keyboard input
	handleKey := func(key int32) {
		switch key {
		case 'p':
			paused = !paused
			state := Executing
			if paused {
				state = Paused
			}
			c.events <- StateChange{CompletedTurns: turn, NewState: state}
		case 's':
			outputWorld()
		case 'q':
			outputWorld()
			quit = true
		case 'k':
			outputWorld()
			quit = true
		}
	}

	// runs until the requested number of turns or until quit
	for turn < p.Turns && !quit {
		if paused {
			select {
			case key, ok := <-keyPresses:
				if !ok {
					keyPresses = nil
					continue
				}
				handleKey(key)
			case <-aliveTicker.C:
				if turn > 0 {
					c.events <- AliveCellsCount{CompletedTurns: turn, CellsCount: currentAlive}
				}
			}
			continue
		}

		select {
		case key, ok := <-keyPresses:
			if !ok {
				keyPresses = nil
				continue
			}
			handleKey(key)
			if quit {
				continue
			}
			if paused {
				continue
			}
		case <-aliveTicker.C:
			if turn > 0 {
				c.events <- AliveCellsCount{CompletedTurns: turn, CellsCount: currentAlive}
			}
			continue
		default:
		}

		// run a single generation with the configured worker count
		changes, alive := runTurn(world, nextWorld, width, height, p.Threads, true)

		if len(changes) > 0 {
			c.events <- CellsFlipped{CompletedTurns: turn, Cells: changes}
		}

		currentAlive = alive
		world, nextWorld = nextWorld, world
		turn++
		c.events <- TurnComplete{CompletedTurns: turn}
	}

	// if the current turn hasn't been saved
	// call outputWorld() to save the board before exiting
	if lastOutputTurn != turn {
		outputWorld()
	}

	// final state and shutdown notifications
	finalAliveCells := worldToAliveCells(world)
	c.events <- FinalTurnComplete{
		CompletedTurns: turn,
		Alive:          finalAliveCells,
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
