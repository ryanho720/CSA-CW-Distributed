package gol

import (
	"fmt"
	"log"
	"net/rpc"
	"sync"
	"time"
)

func runRemoteDistributor(p Params, c distributorChannels) {
	width, height := p.ImageWidth, p.ImageHeight
	world := makeWorld(width, height)
	var worldMu sync.Mutex
	currentTurn := 0
	quitCh := make(chan struct{})
	stopAlive := make(chan struct{})
	var stopAliveOnce sync.Once
	var userQuit bool
	remotePaused := false

	// load the initial board from io
	filename := fmt.Sprintf("%vx%v", width, height)

	// tells the io process which file to read
	c.ioCommand <- ioInput
	c.ioFilename <- filename
	// pulls each byte from the input and writes it into world
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// finds all live cells and puts it into initialAlive
	// if they are alive, emit a CellsFlipped event at turn 0
	initialAlive := worldToAliveCells(world)
	if len(initialAlive) > 0 {
		c.events <- CellsFlipped{CompletedTurns: 0, Cells: initialAlive}
	}

	// sends a StateChange to mark the simulation as Executing at turn 0
	c.events <- StateChange{CompletedTurns: 0, NewState: Executing}

	// connect to the remote engine rpc service
	client, err := rpc.Dial("tcp", p.EngineAddr)
	// if the dial fails, signals the ui with a StateChange to Quitting at turn 0
	if err != nil {
		log.Printf("[Remote] Failed to connect to engine: %v", err)
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}
	defer client.Close()

	// for tracking and streaming events for the correct session
	sessionID := fmt.Sprintf("controller-%d", time.Now().UnixNano())

	// building the payload sent to the remote engine's process rpc
	req := EngineRequest{
		Params: Params{
			Turns:       p.Turns,
			Threads:     p.Threads,
			ImageWidth:  p.ImageWidth,
			ImageHeight: p.ImageHeight,
		},
		World:     worldToBytes(world),
		SessionID: sessionID,
	}

	// holds the rpc response & create a buffer to catch the result
	var resp EngineResponse
	processDone := make(chan error, 1)
	// fire off the long-running Process RPC
	go func() {
		processDone <- client.Call(EngineServiceName+".Process", req, &resp)
	}()

	// writes a world snapshot out via IO
	outputWorld := func(turn int, snapshot [][]byte) {
		if c.ioCommand == nil || c.ioFilename == nil || c.ioOutput == nil || c.ioIdle == nil {
			return
		}
		filename := fmt.Sprintf("%vx%vx%v", width, height, turn)
		c.ioCommand <- ioOutput
		c.ioFilename <- filename
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				c.ioOutput <- snapshot[y][x]
			}
		}
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle
		c.events <- ImageOutputComplete{CompletedTurns: turn, Filename: filename}
	}

	// closed when event is done
	eventsDone := make(chan struct{})
	// streams per turn events from the engine
	// forwards them to the controller ui
	go func() {
		defer close(eventsDone)
		for {
			select {
			case <-quitCh:
				return
			default:
			}
			worldMu.Lock()
			paused := remotePaused
			worldMu.Unlock()
			if paused {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			var eventsResp TurnEventsResponse
			err := client.Call(
				EngineServiceName+".NextTurnEvents",
				TurnEventsRequest{SessionID: sessionID, MaxEvents: 8},
				&eventsResp,
			)
			if err != nil {
				if !userQuit {
					log.Printf("[Remote] TurnEvents RPC failed: %v", err)
				}
				return
			}
			for _, evt := range eventsResp.Events {
				// update local copy of the world for snapshotting
				worldMu.Lock()
				if len(evt.Cells) > 0 {
					for _, cell := range evt.Cells {
						y, x := cell.Y, cell.X
						if y >= 0 && y < height && x >= 0 && x < width {
							if world[y][x] == 255 {
								world[y][x] = 0
							} else {
								world[y][x] = 255
							}
						}
					}
				}
				currentTurn = evt.CompletedTurns
				worldMu.Unlock()

				if len(evt.Cells) > 0 {
					c.events <- CellsFlipped{
						CompletedTurns: evt.CellsCompletedTurns,
						Cells:          evt.Cells,
					}
				}
				c.events <- TurnComplete{CompletedTurns: evt.CompletedTurns}
			}
			if eventsResp.Done {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// snapshot helper that asks the engine for the current world
	snapshot := func() ([][]byte, int, error) {
		var snapResp SnapshotResponse
		err := client.Call(EngineServiceName+".Snapshot", ControlRequest{SessionID: sessionID}, &snapResp)
		if err != nil {
			return nil, 0, err
		}
		world, err := bytesToWorld(snapResp.World, width, height)
		if err != nil {
			return nil, 0, err
		}
		return world, snapResp.CompletedTurns, nil
	}

	callControl := func(method string) error {
		return client.Call(EngineServiceName+"."+method, ControlRequest{SessionID: sessionID}, &struct{}{})
	}

	// allow key handling in remote mode (snapshot/save, quit, pause/resume)
	if c.keyPresses != nil {
		go func() {
			for key := range c.keyPresses {
				switch key {
				case 's':
					worldMu.Lock()
					turn := currentTurn
					worldMu.Unlock()
					if snapWorld, snapTurn, err := snapshot(); err == nil {
						outputWorld(snapTurn, snapWorld)
					} else {
						outputWorld(turn, world)
					}
				case 'q':
					worldMu.Lock()
					userQuit = true
					worldMu.Unlock()
					_ = callControl("Stop")
					if snapWorld, snapTurn, err := snapshot(); err == nil {
						outputWorld(snapTurn, snapWorld)
					} else {
						outputWorld(currentTurn, world)
					}
					c.events <- StateChange{CompletedTurns: currentTurn, NewState: Quitting}
					close(quitCh)
					stopAliveOnce.Do(func() { close(stopAlive) })
					_ = client.Close()
					return
				case 'k':
					worldMu.Lock()
					userQuit = true
					worldMu.Unlock()
					_ = callControl("Stop")
					if snapWorld, snapTurn, err := snapshot(); err == nil {
						outputWorld(snapTurn, snapWorld)
					} else {
						outputWorld(currentTurn, world)
					}
					c.events <- StateChange{CompletedTurns: currentTurn, NewState: Quitting}
					close(quitCh)
					stopAliveOnce.Do(func() { close(stopAlive) })
					_ = client.Close()
					return
				case 'p':
					worldMu.Lock()
					turn := currentTurn
					worldMu.Unlock()
					if !remotePaused {
						if err := callControl("Pause"); err == nil {
							if snapWorld, snapTurn, err := snapshot(); err == nil {
								worldMu.Lock()
								world = snapWorld
								currentTurn = snapTurn
								worldMu.Unlock()
								turn = snapTurn
							}
							worldMu.Lock()
							remotePaused = true
							worldMu.Unlock()
							fmt.Printf("Paused at turn %d\n", turn)
							c.events <- StateChange{CompletedTurns: turn, NewState: Paused}
						}
					} else {
						if err := callControl("Resume"); err == nil {
							if snapWorld, snapTurn, err := snapshot(); err == nil {
								worldMu.Lock()
								world = snapWorld
								currentTurn = snapTurn
								worldMu.Unlock()
								turn = snapTurn
							}
							worldMu.Lock()
							remotePaused = false
							worldMu.Unlock()
							fmt.Println("Continuing")
							c.events <- StateChange{CompletedTurns: turn, NewState: Executing}
						}
					}
				}
			}
		}()
	}

	// ticker triggers periodic alive count rpcs
	ticker := time.NewTicker(2 * time.Second)
	aliveDone := make(chan struct{})
	// counting alive cells
	go func() {
		defer close(aliveDone)
		for {
			select {
			case <-quitCh:
				return
			case <-ticker.C:
				worldMu.Lock()
				paused := remotePaused
				worldMu.Unlock()
				if paused {
					continue
				}
				var aliveResp AliveCountResponse
				err := client.Call(
					EngineServiceName+".AliveCount",
					AliveCountRequest{SessionID: sessionID},
					&aliveResp,
				)
				if err != nil {
					if !userQuit {
						log.Printf("[Remote] AliveCount RPC failed: %v", err)
					}
					return
				}
				if aliveResp.CompletedTurns >= 0 {
					c.events <- AliveCellsCount{
						CompletedTurns: aliveResp.CompletedTurns,
						CellsCount:     aliveResp.AliveCount,
					}
				}
				if aliveResp.Done {
					return
				}
			case <-stopAlive:
				return
			}
		}
	}()

	// wait for the Process rpc to return
	// finishing off
	err = <-processDone
	ticker.Stop()
	stopAliveOnce.Do(func() { close(stopAlive) })
	<-aliveDone
	<-eventsDone
	if err != nil {
		if !userQuit {
			log.Printf("[Remote] Engine RPC failed: %v", err)
			c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
			close(c.events)
			return
		}
	}

	// rebuilds the final board the engine returned
	finalWorld, err := bytesToWorld(resp.World, width, height)
	if err != nil {
		log.Printf("[Remote] Invalid engine response: %v", err)
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}

	// write final board to disk and emit completion events
	outputFilename := fmt.Sprintf("%vx%vx%v", width, height, p.Turns)
	c.ioCommand <- ioOutput
	c.ioFilename <- outputFilename
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c.ioOutput <- finalWorld[y][x]
		}
	}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{CompletedTurns: p.Turns, Filename: outputFilename}

	aliveCells := worldToAliveCells(finalWorld)
	c.events <- FinalTurnComplete{CompletedTurns: p.Turns, Alive: aliveCells}

	c.events <- StateChange{CompletedTurns: p.Turns, NewState: Quitting}
	close(c.events)
}
