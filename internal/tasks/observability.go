package tasks

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"cyber-code/internal/core"
)

const defaultObservationBuffer = 64

// Snapshot is the bounded, safe state exposed to frontends and control-plane
// commands for one child task.
type Snapshot struct {
	ID          string      `json:"id"`
	Agent       string      `json:"agent,omitempty"`
	Description string      `json:"description,omitempty"`
	Status      TaskStatus  `json:"status"`
	Usage       core.Usage  `json:"usage"`
	RecentTool  string      `json:"recent_tool,omitempty"`
	Truncated   bool        `json:"truncated,omitempty"`
	QueueStatus QueueStatus `json:"queue_status,omitempty"`
	Attempts    int         `json:"attempts,omitempty"`
}

type observationHub struct {
	mu       sync.Mutex
	capacity int
	nextID   uint64
	closed   bool
	subs     map[uint64]chan core.Event
	states   map[string]Snapshot
}

func newObservationHub(capacity int) *observationHub {
	if capacity <= 0 {
		capacity = defaultObservationBuffer
	}
	return &observationHub{capacity: capacity, subs: make(map[uint64]chan core.Event), states: make(map[string]Snapshot)}
}

func (hub *observationHub) observe(ctx context.Context) <-chan core.Event {
	if ctx == nil {
		ctx = context.Background()
	}
	channel := make(chan core.Event, hub.capacity)
	hub.mu.Lock()
	if hub.closed {
		close(channel)
		hub.mu.Unlock()
		return channel
	}
	id := hub.nextID
	hub.nextID++
	hub.subs[id] = channel
	hub.mu.Unlock()
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			hub.remove(id)
		}()
	}
	return channel
}

func (hub *observationHub) remove(id uint64) {
	hub.mu.Lock()
	channel, exists := hub.subs[id]
	if exists {
		delete(hub.subs, id)
		close(channel)
	}
	hub.mu.Unlock()
}

func (hub *observationHub) publish(event core.Event) error {
	if event.Subagent == nil || event.Subagent.TaskID == "" {
		return fmt.Errorf("subagent observation requires a task ID")
	}
	if nested := event.Subagent.Event; nested != nil && isSubagentEvent(nested.Type) {
		return fmt.Errorf("nested subagent observations are not allowed")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return context.Canceled
	}
	snapshot := hub.updateSnapshot(event.Subagent)
	payload := *event.Subagent
	payload.Agent = firstNonEmpty(payload.Agent, snapshot.Agent)
	payload.Description = firstNonEmpty(payload.Description, snapshot.Description)
	payload.Status = firstNonEmpty(payload.Status, string(snapshot.Status))
	payload.RecentTool = firstNonEmpty(payload.RecentTool, snapshot.RecentTool)
	payload.Truncated = payload.Truncated || snapshot.Truncated
	usage := snapshot.Usage
	payload.Usage = &usage
	event.Subagent = &payload
	dropped := false
	for _, subscriber := range hub.subs {
		select {
		case subscriber <- event:
		default:
			dropped = true
		}
	}
	if dropped {
		snapshot.Truncated = true
		hub.states[snapshot.ID] = snapshot
	}
	return nil
}

func (hub *observationHub) updateSnapshot(payload *core.SubagentEvent) Snapshot {
	snapshot := hub.states[payload.TaskID]
	snapshot.ID = payload.TaskID
	snapshot.Agent = firstNonEmpty(payload.Agent, snapshot.Agent)
	snapshot.Description = firstNonEmpty(payload.Description, snapshot.Description)
	if payload.Status != "" {
		snapshot.Status = TaskStatus(payload.Status)
	}
	if payload.Usage != nil {
		snapshot.Usage = *payload.Usage
	}
	snapshot.RecentTool = firstNonEmpty(payload.RecentTool, snapshot.RecentTool)
	snapshot.Truncated = snapshot.Truncated || payload.Truncated
	if nested := payload.Event; nested != nil {
		switch nested.Type {
		case core.EventUsage:
			if nested.Usage != nil {
				snapshot.Usage.InputTokens += nested.Usage.InputTokens
				snapshot.Usage.OutputTokens += nested.Usage.OutputTokens
				snapshot.Usage.CacheReadInputTokens += nested.Usage.CacheReadInputTokens
				snapshot.Usage.CacheCreationInputTokens += nested.Usage.CacheCreationInputTokens
			}
		case core.EventToolCall:
			if nested.ToolCall != nil {
				snapshot.RecentTool = nested.ToolCall.Name
			}
		}
	}
	hub.states[snapshot.ID] = snapshot
	return snapshot
}

func (hub *observationHub) snapshotsCopy() []Snapshot {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	result := make([]Snapshot, 0, len(hub.states))
	for _, snapshot := range hub.states {
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (hub *observationHub) snapshots() []Snapshot { return hub.snapshotsCopy() }

func (hub *observationHub) recordQueue(taskID string, status QueueStatus, attempts int) {
	if hub == nil || taskID == "" {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return
	}
	snapshot := hub.states[taskID]
	snapshot.ID = taskID
	snapshot.QueueStatus = status
	if attempts > snapshot.Attempts {
		snapshot.Attempts = attempts
	}
	hub.states[taskID] = snapshot
}

func (hub *observationHub) close() {
	hub.mu.Lock()
	if hub.closed {
		hub.mu.Unlock()
		return
	}
	hub.closed = true
	for id, subscriber := range hub.subs {
		delete(hub.subs, id)
		close(subscriber)
	}
	hub.mu.Unlock()
}

func isSubagentEvent(eventType core.EventType) bool {
	return eventType == core.EventSubagentStarted || eventType == core.EventSubagentEvent || eventType == core.EventSubagentStatus
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
