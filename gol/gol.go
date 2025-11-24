package gol

import "strings"

// client: go run ./engine -listen :6000
// server: go run . -engine localhost:6000
const (
	defaultLocalEngineAddr = "localhost:6000"
	defaultAwsEngineAddr   = "AWS_ENGINE_ADDR_PLACEHOLDER"
)

// Params provides the details of how to run the Game of Life and which image to load.
type Params struct {
	Turns       int
	Threads     int
	ImageWidth  int
	ImageHeight int
	EngineAddr  string
}

// Run starts the processing of Game of Life. It should initialise channels and goroutines.
func Run(p Params, events chan<- Event, keyPresses <-chan int32) {
	p.EngineAddr = resolveEngineAddr(p.EngineAddr)
	ioCommand := make(chan ioCommand)
	ioIdle := make(chan bool)
	ioFilename := make(chan string)
	ioOutput := make(chan uint8)
	ioInput := make(chan uint8)

	ioChannels := ioChannels{
		command:  ioCommand,
		idle:     ioIdle,
		filename: ioFilename,
		output:   ioOutput,
		input:    ioInput,
	}
	go startIo(p, ioChannels)

	distributorChannels := distributorChannels{
		events:     events,
		ioCommand:  ioCommand,
		ioIdle:     ioIdle,
		ioFilename: ioFilename,
		ioOutput:   ioOutput,
		ioInput:    ioInput,
		keyPresses: keyPresses,
	}
	distributor(p, distributorChannels)

	close(ioCommand)
}

func resolveEngineAddr(raw string) string {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return ""
	}

	lower := strings.ToLower(addr)
	switch lower {
	case "local", "localhost":
		return defaultLocalEngineAddr
	case "aws":
		return defaultAwsEngineAddr
	}

	return addr
}
