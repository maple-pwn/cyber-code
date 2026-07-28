// Package collaboration contains bounded coordination primitives for agents.
package collaboration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Message struct {
	ID        string    `json:"id"`
	Sequence  uint64    `json:"sequence"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
type Options struct {
	MaxMessages  int
	MaxBodyBytes int
}
type mailboxState struct {
	Messages []Message         `json:"messages"`
	Acks     map[string]uint64 `json:"acks"`
}
type Mailbox struct {
	path                      string
	maxMessages, maxBodyBytes int
	mu                        sync.Mutex
}

func NewMailbox(directory string, options Options) (*Mailbox, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("mailbox directory is required")
	}
	if options.MaxMessages <= 0 {
		options.MaxMessages = 4096
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = 64 << 10
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	return &Mailbox{path: filepath.Join(directory, "mailbox.json"), maxMessages: options.MaxMessages, maxBodyBytes: options.MaxBodyBytes}, nil
}
func (box *Mailbox) Send(ctx context.Context, message Message) (Message, error) {
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	message.From, message.To, message.Body = strings.TrimSpace(message.From), strings.TrimSpace(message.To), strings.TrimSpace(message.Body)
	if message.From == "" || message.To == "" || message.Body == "" {
		return Message{}, fmt.Errorf("mailbox message requires from, to, and body")
	}
	if len([]byte(message.Body)) > box.maxBodyBytes {
		return Message{}, fmt.Errorf("mailbox body exceeds %d bytes", box.maxBodyBytes)
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	state, err := box.read()
	if err != nil {
		return Message{}, err
	}
	if len(state.Messages) >= box.maxMessages {
		return Message{}, fmt.Errorf("mailbox exceeds %d messages", box.maxMessages)
	}
	var idBytes [8]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return Message{}, err
	}
	message.ID = "msg-" + hex.EncodeToString(idBytes[:])
	message.Sequence = uint64(len(state.Messages) + 1)
	message.CreatedAt = time.Now().UTC()
	state.Messages = append(state.Messages, message)
	if err := box.write(state); err != nil {
		return Message{}, err
	}
	return message, nil
}
func (box *Mailbox) Poll(ctx context.Context, recipient string, after uint64, limit int) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	state, err := box.read()
	if err != nil {
		return nil, err
	}
	result := make([]Message, 0, limit)
	for _, message := range state.Messages {
		if message.To == recipient && message.Sequence > after && message.Sequence > state.Acks[recipient] {
			result = append(result, message)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}
func (box *Mailbox) Ack(ctx context.Context, recipient string, sequence uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	state, err := box.read()
	if err != nil {
		return err
	}
	if sequence > state.Acks[recipient] {
		state.Acks[recipient] = sequence
	}
	return box.write(state)
}
func (box *Mailbox) read() (mailboxState, error) {
	data, err := os.ReadFile(box.path)
	if os.IsNotExist(err) {
		return mailboxState{Acks: map[string]uint64{}}, nil
	}
	if err != nil {
		return mailboxState{}, err
	}
	var state mailboxState
	if err := json.Unmarshal(data, &state); err != nil {
		return mailboxState{}, err
	}
	if state.Acks == nil {
		state.Acks = map[string]uint64{}
	}
	return state, nil
}
func (box *Mailbox) write(state mailboxState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(box.path), ".mailbox-*")
	if err != nil {
		return err
	}
	path := temp.Name()
	defer os.Remove(path)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(path, box.path)
}
