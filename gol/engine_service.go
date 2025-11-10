package gol

import (
	"fmt"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
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

// TurnEventsRequest asks for new turn events for a session.
type TurnEventsRequest struct {
	SessionID string
	MaxEvents int
}

// TurnEventsResponse contains streamed turn events.
type TurnEventsResponse struct {
	Events []TurnEvent
	Done   bool
}

// TurnEvent represents cells flipped for a turn and the completed turn count.
type TurnEvent struct {
	CellsCompletedTurns int
	CompletedTurns      int
	Cells               []util.Cell
}

type engineSession struct {
	CompletedTurns int
	AliveCount     int
	Done           bool
	events         []TurnEvent
	eventIndex     int
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

	callback := func(turn, alive int, cells []util.Cell) {
		event := TurnEvent{
			CellsCompletedTurns: turn - 1,
			CompletedTurns:      turn,
			Cells:               append([]util.Cell(nil), cells...),
		}
		s.appendEvent(sessionID, event)
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

// NextTurnEvents streams queued turn events to the controller.
func (s *EngineService) NextTurnEvents(req TurnEventsRequest, resp *TurnEventsResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[req.SessionID]
	if !ok {
		resp.Done = false
		return nil
	}

	if req.MaxEvents <= 0 {
		req.MaxEvents = 4
	}

	if session.eventIndex < len(session.events) {
		end := session.eventIndex + req.MaxEvents
		if end > len(session.events) {
			end = len(session.events)
		}
		resp.Events = append(resp.Events, session.events[session.eventIndex:end]...)
		session.eventIndex = end

		// Trim events to avoid unbounded growth.
		if session.eventIndex > 0 && session.eventIndex == len(session.events) {
			session.events = session.events[:0]
			session.eventIndex = 0
		}
	}

	resp.Done = session.Done && session.eventIndex == len(session.events)
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

func (s *EngineService) appendEvent(id string, event TurnEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		session = &engineSession{}
		s.sessions[id] = session
	}
	session.events = append(session.events, event)
}

func evolveWorld(p Params, world [][]byte, status func(turn int, alive int, cells []util.Cell)) ([][]byte, int) {
	width := p.ImageWidth
	height := p.ImageHeight
	next := makeWorld(width, height)
	if p.Turns <= 0 {
		return world, len(worldToAliveCells(world))
	}

	var alive int
	for turn := 0; turn < p.Turns; turn++ {
		changes, alive := runTurn(world, next, width, height, p.Threads, true)
		world, next = next, world
		if status != nil {
			status(turn+1, alive, changes)
		}
	}

	return world, alive
}
