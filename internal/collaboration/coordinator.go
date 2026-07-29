package collaboration

import (
	"context"
	"fmt"
)

// Coordinator exposes only explicit task and message operations. The write
// lease serializes workspace mutations across locally coordinated agents.
type Coordinator struct {
	Board   *Board
	Mailbox *Mailbox
	write   chan struct{}
}

func NewCoordinator(directory string) (*Coordinator, error) {
	board, err := NewBoard(directory, BoardOptions{})
	if err != nil {
		return nil, fmt.Errorf("open task board: %w", err)
	}
	mailbox, err := NewMailbox(directory, Options{})
	if err != nil {
		return nil, fmt.Errorf("open mailbox: %w", err)
	}
	lease := make(chan struct{}, 1)
	lease <- struct{}{}
	return &Coordinator{Board: board, Mailbox: mailbox, write: lease}, nil
}

func (coordinator *Coordinator) Send(ctx context.Context, message Message) (Message, error) {
	if coordinator == nil || coordinator.Mailbox == nil {
		return Message{}, fmt.Errorf("coordinator mailbox is unavailable")
	}
	return coordinator.Mailbox.Send(ctx, message)
}

func (coordinator *Coordinator) Receive(ctx context.Context, recipient string, after uint64, limit int) ([]Message, error) {
	if coordinator == nil || coordinator.Mailbox == nil {
		return nil, fmt.Errorf("coordinator mailbox is unavailable")
	}
	return coordinator.Mailbox.Poll(ctx, recipient, after, limit)
}

func (coordinator *Coordinator) WithWorkspaceWrite(ctx context.Context, action func() error) error {
	if coordinator == nil || coordinator.write == nil || action == nil {
		return fmt.Errorf("coordinator write action is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-coordinator.write:
	}
	defer func() { coordinator.write <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return action()
}
