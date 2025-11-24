package gol

import (
	"fmt"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

const EngineServiceName = "GolEngine"

// EngineRequest is sent by the controller to request processing of turns
type EngineRequest struct {
	Params    Params
	World     []byte
	SessionID string
}

// EngineResponse contains the final world after processing
type EngineResponse struct {
	World     []byte
	SessionID string
}

// ControlRequest targets a specific session for pause/resume/stop/snapshot.
type ControlRequest struct {
	SessionID string
}

// SnapshotResponse returns the current world and metadata.
type SnapshotResponse struct {
	World          []byte
	CompletedTurns int
	Done           bool
}

// AliveCountRequest queries the progress of a running session
type AliveCountRequest struct {
	SessionID string
}

// AliveCountResponse contains the number of completed turns and alive cells
type AliveCountResponse struct {
	CompletedTurns int
	AliveCount     int
	Done           bool
}

// TurnEventsRequest asks for new turn events for a session
type TurnEventsRequest struct {
	SessionID string
	MaxEvents int
}

// TurnEventsResponse contains streamed turn events
type TurnEventsResponse struct {
	Events []TurnEvent
	Done   bool
}

// TurnEvent represents cells flipped for a turn and the completed turn count
type TurnEvent struct {
	CellsCompletedTurns int
	CompletedTurns      int
	Cells               []util.Cell
}

type engineSession struct {
	CompletedTurns int
	AliveCount     int
	Done           bool
	events         []TurnEvent // queued turn events ready to be streamed to a controller
	eventIndex     int
	paused         bool
	stopRequested  bool
	world          [][]byte
}

// EngineService exposes RPC methods for processing Game of Life turns remotely
type EngineService struct {
	mu       sync.RWMutex
	sessions map[string]*engineSession
}

// NewEngineService constructs a new EngineService
func NewEngineService() *EngineService {
	return &EngineService{
		sessions: make(map[string]*engineSession),
	}
}

// process evolves the supplied world for Params.Turns turns
func (s *EngineService) Process(req EngineRequest, resp *EngineResponse) error {
	width := req.Params.ImageWidth
	height := req.Params.ImageHeight
	// decode incoming flat world
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
	s.setWorld(sessionID, world)

	// callback captures per-turn updates for streaming
	callback := func(turn, alive int, cells []util.Cell) {
		event := TurnEvent{
			CellsCompletedTurns: turn - 1,
			CompletedTurns:      turn,
			Cells:               append([]util.Cell(nil), cells...),
		}
		s.appendEvent(sessionID, event)
		s.setSession(sessionID, turn, alive, false)
		s.updateWorld(sessionID, world)
	}

	finalWorld, finalAlive := evolveWorld(req.Params, world, func(turn, alive int, cells []util.Cell) bool {
		callback(turn, alive, cells)
		for {
			s.mu.RLock()
			sess := s.sessions[sessionID]
			paused := sess != nil && sess.paused
			stop := sess != nil && sess.stopRequested
			s.mu.RUnlock()
			if stop {
				return false
			}
			if !paused {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		return true
	})
	s.mu.RLock()
	sess := s.sessions[sessionID]
	completed := req.Params.Turns
	aliveCount := finalAlive
	if sess != nil {
		completed = sess.CompletedTurns
		aliveCount = sess.AliveCount
	}
	s.mu.RUnlock()

	s.setSession(sessionID, completed, aliveCount, true)

	resp.World = worldToBytes(finalWorld)
	return nil
}

// AliveCount returns the latest known alive cell count for a session
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

	// drain up to MaxEvents from the buffered event queue
	if session.eventIndex < len(session.events) {
		end := session.eventIndex + req.MaxEvents
		if end > len(session.events) {
			end = len(session.events)
		}
		resp.Events = append(resp.Events, session.events[session.eventIndex:end]...)
		session.eventIndex = end

		// trim events to avoid unbounded growth
		if session.eventIndex > 0 && session.eventIndex == len(session.events) {
			session.events = session.events[:0]
			session.eventIndex = 0
		}
	}

	resp.Done = session.Done && session.eventIndex == len(session.events)
	return nil
}

// Pause halts processing of a session until Resume is called.
func (s *EngineService) Pause(req ControlRequest, _ *struct{}) error {
	s.mu.Lock()
	if sess, ok := s.sessions[req.SessionID]; ok {
		sess.paused = true
	}
	s.mu.Unlock()
	return nil
}

// Resume continues processing of a paused session.
func (s *EngineService) Resume(req ControlRequest, _ *struct{}) error {
	s.mu.Lock()
	if sess, ok := s.sessions[req.SessionID]; ok {
		sess.paused = false
	}
	s.mu.Unlock()
	return nil
}

// Stop requests that a session terminate early.
func (s *EngineService) Stop(req ControlRequest, _ *struct{}) error {
	s.mu.Lock()
	if sess, ok := s.sessions[req.SessionID]; ok {
		sess.stopRequested = true
	}
	s.mu.Unlock()
	return nil
}

// Snapshot returns the current world for a session.
func (s *EngineService) Snapshot(req ControlRequest, resp *SnapshotResponse) error {
	s.mu.RLock()
	sess, ok := s.sessions[req.SessionID]
	s.mu.RUnlock()
	if !ok || sess == nil {
		return fmt.Errorf("session not found")
	}

	resp.CompletedTurns = sess.CompletedTurns
	resp.Done = sess.Done
	if sess.world != nil {
		resp.World = worldToBytes(sess.world)
	}
	return nil
}

// updates the per session record
func (s *EngineService) setSession(id string, turns, alive int, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		session = &engineSession{}
		s.sessions[id] = session
		session.paused = false
		session.stopRequested = false
	}
	if turns >= 0 {
		session.CompletedTurns = turns
	}
	session.AliveCount = alive
	if done {
		session.Done = true
	}
}

// records a turn event for a session
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

func (s *EngineService) setWorld(id string, world [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		session = &engineSession{}
		s.sessions[id] = session
	}
	session.world = world
}

func (s *EngineService) updateWorld(id string, world [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return
	}
	session.world = world
}

// runs gol for p.Turns steps
func evolveWorld(p Params, world [][]byte, status func(turn int, alive int, cells []util.Cell) bool) ([][]byte, int) {
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
			if ok := status(turn+1, alive, changes); !ok {
				return world, alive
			}
		}
	}

	return world, alive
}
