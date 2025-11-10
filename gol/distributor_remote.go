package gol

import (
	"fmt"
	"log"
	"net/rpc"
	"time"
)

func runRemoteDistributor(p Params, c distributorChannels) {
	if c.keyPresses != nil {
		go func() {
			for range c.keyPresses {
			}
		}()
	}

	width, height := p.ImageWidth, p.ImageHeight
	world := makeWorld(width, height)

	filename := fmt.Sprintf("%vx%v", width, height)

	c.ioCommand <- ioInput
	c.ioFilename <- filename
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	c.events <- StateChange{CompletedTurns: 0, NewState: Executing}

	client, err := rpc.Dial("tcp", p.EngineAddr)
	if err != nil {
		log.Printf("[Remote] Failed to connect to engine: %v", err)
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}
	defer client.Close()

	sessionID := fmt.Sprintf("controller-%d", time.Now().UnixNano())

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

	var resp EngineResponse
	processDone := make(chan error, 1)
	go func() {
		processDone <- client.Call(EngineServiceName+".Process", req, &resp)
	}()

	ticker := time.NewTicker(2 * time.Second)
	stopAlive := make(chan struct{})
	aliveDone := make(chan struct{})
	go func() {
		defer close(aliveDone)
		for {
			select {
			case <-ticker.C:
				var aliveResp AliveCountResponse
				err := client.Call(
					EngineServiceName+".AliveCount",
					AliveCountRequest{SessionID: sessionID},
					&aliveResp,
				)
				if err != nil {
					log.Printf("[Remote] AliveCount RPC failed: %v", err)
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

	err = <-processDone
	ticker.Stop()
	close(stopAlive)
	<-aliveDone
	if err != nil {
		log.Printf("[Remote] Engine RPC failed: %v", err)
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}

	finalWorld, err := bytesToWorld(resp.World, width, height)
	if err != nil {
		log.Printf("[Remote] Invalid engine response: %v", err)
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}

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
