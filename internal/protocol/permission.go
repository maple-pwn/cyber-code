package protocol

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"cyber-code/internal/permissions"
)

var ErrUnknownPermission = errors.New("unknown permission request")

type PermissionPrompt struct {
	ID      string              `json:"id"`
	Request permissions.Request `json:"request"`
}

type PermissionBroker struct {
	next      atomic.Uint64
	requests  chan PermissionPrompt
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	pending   map[string]chan permissions.Decision
}

func NewPermissionBroker(capacity int) *PermissionBroker {
	if capacity <= 0 {
		capacity = 16
	}
	return &PermissionBroker{requests: make(chan PermissionPrompt, capacity), closed: make(chan struct{}), pending: make(map[string]chan permissions.Decision)}
}

func (broker *PermissionBroker) Requests() <-chan PermissionPrompt { return broker.requests }

func (broker *PermissionBroker) Confirm(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id := fmt.Sprintf("permission-%d", broker.next.Add(1))
	reply := make(chan permissions.Decision, 1)
	broker.mu.Lock()
	select {
	case <-broker.closed:
		broker.mu.Unlock()
		return deniedPermission("permission broker is closed"), nil
	default:
	}
	broker.pending[id] = reply
	broker.mu.Unlock()
	defer func() { broker.mu.Lock(); delete(broker.pending, id); broker.mu.Unlock() }()
	select {
	case broker.requests <- PermissionPrompt{ID: id, Request: request}:
	case <-ctx.Done():
		return permissions.Decision{}, ctx.Err()
	case <-broker.closed:
		return deniedPermission("permission broker is closed"), nil
	}
	select {
	case decision := <-reply:
		return decision, nil
	case <-ctx.Done():
		return permissions.Decision{}, ctx.Err()
	case <-broker.closed:
		return deniedPermission("permission broker disconnected"), nil
	}
}

func (broker *PermissionBroker) Respond(id string, decision permissions.Decision) error {
	if decision.Behavior != permissions.PermissionBehaviorAllow && decision.Behavior != permissions.PermissionBehaviorDeny {
		return errors.New("invalid permission decision")
	}
	broker.mu.Lock()
	reply, ok := broker.pending[id]
	broker.mu.Unlock()
	if !ok {
		return ErrUnknownPermission
	}
	select {
	case reply <- decision:
		return nil
	default:
		return ErrUnknownPermission
	}
}

func (broker *PermissionBroker) Close() { broker.closeOnce.Do(func() { close(broker.closed) }) }

func (broker *PermissionBroker) Disconnect() {
	broker.mu.Lock()
	replies := make([]chan permissions.Decision, 0, len(broker.pending))
	for _, reply := range broker.pending {
		replies = append(replies, reply)
	}
	broker.mu.Unlock()
	for _, reply := range replies {
		select {
		case reply <- deniedPermission("protocol client disconnected"):
		default:
		}
	}
	for {
		select {
		case <-broker.requests:
			continue
		default:
			return
		}
	}
}

func deniedPermission(reason string) permissions.Decision {
	return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: reason}
}
