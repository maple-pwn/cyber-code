package protocol

import (
	"errors"
	"io"
	"sync"
	"time"
)

var ErrSlowConsumer = errors.New("protocol consumer is too slow")

type connectionOutput struct {
	writer io.Writer
	codec  Codec
	queue  chan Response
	done   chan struct{}
	mu     sync.Mutex
	closed bool
	err    error
}

func newConnectionOutput(writer io.Writer, codec Codec, capacity int) *connectionOutput {
	if capacity <= 0 {
		capacity = 64
	}
	output := &connectionOutput{writer: writer, codec: codec, queue: make(chan Response, capacity), done: make(chan struct{})}
	go output.run()
	return output
}

func (output *connectionOutput) Send(response Response) error {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.err != nil {
		return output.err
	}
	if output.closed {
		return io.ErrClosedPipe
	}
	select {
	case output.queue <- response:
		return nil
	case <-time.After(50 * time.Millisecond):
		return ErrSlowConsumer
	}
}

func (output *connectionOutput) Close() error {
	output.mu.Lock()
	if !output.closed {
		output.closed = true
		close(output.queue)
	}
	output.mu.Unlock()
	select {
	case <-output.done:
		output.mu.Lock()
		defer output.mu.Unlock()
		return output.err
	case <-time.After(50 * time.Millisecond):
		return ErrSlowConsumer
	}
}

func (output *connectionOutput) run() {
	defer close(output.done)
	for response := range output.queue {
		if err := output.codec.Encode(output.writer, response); err != nil {
			output.mu.Lock()
			output.err = err
			output.mu.Unlock()
			return
		}
	}
}
