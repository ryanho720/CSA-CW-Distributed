package gol

import (
	"fmt"
	"sync"
	"time"
)

const EngineServiceName = "GolEngine"

// EngineRequest is sent by the controller to request processing of turns.
type EngineRequest struct {
	Params    Params
	World     []byte
	SessionID string
}

// EngineResponse contains the final world after processing.
type EngineResponse struct {
	World     []byte
	SessionID string
}

// AliveCountRequest queries the progress of a running session.
type AliveCountRequest struct {
	SessionID string
}

// AliveCountResponse contains the number of completed turns and alive cells.
type AliveCountResponse struct {
	CompletedTurns int
	AliveCount     int
	Done           bool
}

type engineSession struct {
	CompletedTurns int
	AliveCount     int
	Done           bool
}

// EngineService exposes RPC methods for processing Game of Life turns remotely.
type EngineService struct {
	mu       sync.RWMutex
	sessions map[string]*engineSession
}

// NewEngineService constructs a new EngineService.
func NewEngineService() *EngineService {
	return &EngineService{
		sessions: make(map[string]*engineSession),
	}
}

// Process evolves the supplied world for Params.Turns turns.
func (s *EngineService) Process(req EngineRequest, resp *EngineResponse) error {
	width := req.Params.ImageWidth
	height := req.Params.ImageHeight
	world, err := bytesToWorld(req.World, width, height)
	if err != nil {
		return err
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	resp.SessionID = sessionID

	initialAlive := len(worldToAliveCells(world))
	s.setSession(sessionID, 0, initialAlive, false)

	callback := func(turn, alive int) {
		s.setSession(sessionID, turn, alive, false)
	}

	finalWorld, finalAlive := evolveWorld(req.Params, world, callback)
	s.setSession(sessionID, req.Params.Turns, finalAlive, true)

	resp.World = worldToBytes(finalWorld)
	return nil
}

// AliveCount returns the latest known alive cell count for a session.
func (s *EngineService) AliveCount(req AliveCountRequest, resp *AliveCountResponse) error {
	s.mu.RLock()
	session, ok := s.sessions[req.SessionID]
	s.mu.RUnlock()
	if !ok {
		resp.CompletedTurns = -1
		resp.AliveCount = 0
		resp.Done = true
		return nil
	}
	resp.CompletedTurns = session.CompletedTurns
	resp.AliveCount = session.AliveCount
	resp.Done = session.Done
	return nil
}

func (s *EngineService) setSession(id string, turns, alive int, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		session = &engineSession{}
		s.sessions[id] = session
	}
	if turns >= 0 {
		session.CompletedTurns = turns
	}
	session.AliveCount = alive
	if done {
		session.Done = true
	}
}

func evolveWorld(p Params, world [][]byte, status func(turn, alive int)) ([][]byte, int) {
	width := p.ImageWidth
	height := p.ImageHeight
	next := makeWorld(width, height)
	if p.Turns <= 0 {
		return world, len(worldToAliveCells(world))
	}

	var alive int
	for turn := 0; turn < p.Turns; turn++ {
		_, alive = runTurn(world, next, width, height, p.Threads, false)
		world, next = next, world
		if status != nil {
			status(turn+1, alive)
		}
	}

	return world, alive
}
