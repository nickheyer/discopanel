package rcon

import (
	"context"
	"fmt"

	"github.com/jltobler/go-rcon"
)

type rconResult struct {
	output string
	err    error
}

func SendCommand(ctx context.Context, RCONHost string, RCONPort int, RCONPassword string, command string) (string, error) {
	// initialize Client
	rconClient := rcon.NewClient(fmt.Sprintf("rcon://%s:%d", RCONHost, RCONPort), RCONPassword)

	// run Command in a goroutine to allow for timeout handling
	resultCh := make(chan rconResult, 1)
	go func() {
		output, sendErr := rconClient.Send(command)
		resultCh <- rconResult{output: output, err: sendErr}
	}()

	// wait for either the command result or a timeout
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return "", result.err
		}
		return result.output, nil
	}
}
