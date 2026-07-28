package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

var (
	ErrInvalidTransition = errors.New("invalid task state transition")
	ErrInvalidTaskUpdate = errors.New("invalid task update")
)

// =============================================================================
// Task Registry
// =============================================================================

// Registry manages all active tasks.
type Registry struct {
	mu     sync.RWMutex
	tasks  map[string]TaskState
	output map[string]*taskOutput
}

type taskOutput struct {
	data        []byte
	startOffset int
	truncated   bool
}
type OutputPage struct {
	Data        []byte
	StartOffset int
	NextOffset  int
	Total       int
	Truncated   bool
}

// NewRegistry creates a new task registry.
func NewRegistry() *Registry {
	return &Registry{
		tasks:  make(map[string]TaskState),
		output: make(map[string]*taskOutput),
	}
}

// Register adds a task to the registry.
func (r *Registry) Register(task TaskState) error {
	if task == nil || task.GetBase() == nil || task.GetBase().ID == "" {
		return fmt.Errorf("task and task ID are required")
	}
	cloned := cloneTaskState(task)
	if cloned == nil {
		return fmt.Errorf("unsupported task state type %T", task)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	id := task.GetBase().ID
	if _, exists := r.tasks[id]; exists {
		return fmt.Errorf("task %s already exists", id)
	}

	r.tasks[id] = cloned
	r.output[id] = &taskOutput{}
	return nil
}

func (r *Registry) AppendOutput(taskID string, data []byte, maximum int) error {
	if maximum <= 0 {
		return fmt.Errorf("maximum output size must be positive")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	output, ok := r.output[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	total := output.startOffset + len(output.data) + len(data)
	if len(data) >= maximum {
		output.data = append([]byte(nil), data[len(data)-maximum:]...)
		output.startOffset = total - maximum
		output.truncated = total > maximum
		return nil
	}
	if dropped := len(output.data) + len(data) - maximum; dropped > 0 {
		copy(output.data, output.data[dropped:])
		output.data = output.data[:len(output.data)-dropped]
		output.startOffset += dropped
		output.truncated = true
	}
	output.data = append(output.data, data...)
	return nil
}

func (r *Registry) ReadOutput(taskID string, offset, limit int) (OutputPage, error) {
	if offset < 0 || limit <= 0 {
		return OutputPage{}, fmt.Errorf("invalid output page")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	output, ok := r.output[taskID]
	if !ok {
		return OutputPage{}, fmt.Errorf("task %s not found", taskID)
	}
	total := output.startOffset + len(output.data)
	if offset < output.startOffset {
		offset = output.startOffset
	}
	if offset > total {
		offset = total
	}
	end := total
	if limit <= total-offset {
		end = offset + limit
	}
	if end > total {
		end = total
	}
	startIndex := offset - output.startOffset
	endIndex := end - output.startOffset
	return OutputPage{
		Data: append([]byte(nil), output.data[startIndex:endIndex]...), StartOffset: output.startOffset,
		NextOffset: end, Total: total, Truncated: output.truncated,
	}, nil
}

// Unregister removes a task from the registry.
func (r *Registry) Unregister(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.tasks, taskID)
	delete(r.output, taskID)
}

// Get retrieves a task by ID.
func (r *Registry) Get(taskID string) TaskState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return cloneTaskState(r.tasks[taskID])
}

// GetAll returns all tasks.
func (r *Registry) GetAll() map[string]TaskState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]TaskState)
	for k, v := range r.tasks {
		result[k] = cloneTaskState(v)
	}
	return result
}

// Update updates a task's state.
func (r *Registry) Update(taskID string, updateFn func(TaskState) TaskState) error {
	if updateFn == nil {
		return fmt.Errorf("%w: update function is required", ErrInvalidTaskUpdate)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	task, exists := r.tasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	before := *task.GetBase()
	beforeType := reflect.TypeOf(task)
	updated := updateFn(cloneTaskState(task))
	if updated == nil || updated.GetBase() == nil {
		return fmt.Errorf("%w: update returned no task", ErrInvalidTaskUpdate)
	}
	if updated.GetBase().Status != before.Status {
		return fmt.Errorf("%w: %w: Update cannot change task status", ErrInvalidTaskUpdate, ErrInvalidTransition)
	}
	if updated.GetBase().ID != before.ID || updated.GetBase().Type != before.Type || reflect.TypeOf(updated) != beforeType {
		return fmt.Errorf("%w: Update cannot change task identity or type", ErrInvalidTaskUpdate)
	}
	r.tasks[taskID] = cloneTaskState(updated)
	return nil
}

func (r *Registry) Transition(taskID string, status TaskStatus, cause error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	current := task.GetBase().Status
	if !allowedTransition(current, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current, status)
	}
	base := task.GetBase()
	base.Status = status
	if status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled {
		now := time.Now()
		base.EndTime = &now
	}
	if cause != nil {
		setTaskError(task, cause.Error())
	}
	return nil
}

func allowedTransition(from, to TaskStatus) bool {
	switch from {
	case TaskStatusPending:
		return to == TaskStatusRunning || to == TaskStatusCancelled
	case TaskStatusRunning:
		return to == TaskStatusCompleted || to == TaskStatusFailed || to == TaskStatusCancelled
	default:
		return false
	}
}

func setTaskError(task TaskState, message string) {
	switch typed := task.(type) {
	case *LocalAgentTaskState:
		typed.Error = message
	case *LocalShellTaskState:
		typed.Error = message
	case *RemoteAgentTaskState:
		typed.Error = message
	}
}

func cloneTaskState(task TaskState) TaskState {
	if task == nil {
		return nil
	}
	switch typed := task.(type) {
	case *LocalShellTaskState:
		clone := *typed
		clone.TaskStateBase = cloneTaskBase(typed.TaskStateBase)
		if typed.ExitCode != nil {
			value := *typed.ExitCode
			clone.ExitCode = &value
		}
		return &clone
	case *LocalAgentTaskState:
		clone := *typed
		clone.TaskStateBase = cloneTaskBase(typed.TaskStateBase)
		clone.Result = cloneTaskValue(typed.Result)
		if typed.Progress != nil {
			clone.Progress = cloneTaskValue(typed.Progress).(*AgentProgress)
		}
		if typed.Messages != nil {
			clone.Messages = cloneTaskValue(typed.Messages).([]interface{})
		}
		clone.PendingMessages = append([]string(nil), typed.PendingMessages...)
		if typed.EvictAfter != nil {
			value := *typed.EvictAfter
			clone.EvictAfter = &value
		}
		return &clone
	case *RemoteAgentTaskState:
		clone := *typed
		clone.TaskStateBase = cloneTaskBase(typed.TaskStateBase)
		return &clone
	default:
		return nil
	}
}

type cloneTaskVisit struct {
	typeOf  reflect.Type
	pointer uintptr
	length  int
}

func cloneTaskValue(value interface{}) interface{} {
	cloned := cloneTaskData(reflect.ValueOf(value), make(map[cloneTaskVisit]reflect.Value))
	if !cloned.IsValid() {
		return nil
	}
	return cloned.Interface()
}

func cloneTaskData(value reflect.Value, seen map[cloneTaskVisit]reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type()).Elem()
		cloned.Set(cloneTaskData(value.Elem(), seen))
		return cloned
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		visit := cloneTaskVisit{typeOf: value.Type(), pointer: value.Pointer()}
		if cloned, ok := seen[visit]; ok {
			return cloned
		}
		cloned := reflect.New(value.Type().Elem())
		seen[visit] = cloned
		cloned.Elem().Set(cloneTaskData(value.Elem(), seen))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		visit := cloneTaskVisit{typeOf: value.Type(), pointer: value.Pointer()}
		if cloned, ok := seen[visit]; ok {
			return cloned
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		seen[visit] = cloned
		iterator := value.MapRange()
		for iterator.Next() {
			cloned.SetMapIndex(iterator.Key(), cloneTaskData(iterator.Value(), seen))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		visit := cloneTaskVisit{typeOf: value.Type(), pointer: value.Pointer(), length: value.Len()}
		if cloned, ok := seen[visit]; ok {
			return cloned
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		seen[visit] = cloned
		for index := 0; index < value.Len(); index++ {
			cloned.Index(index).Set(cloneTaskData(value.Index(index), seen))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			cloned.Index(index).Set(cloneTaskData(value.Index(index), seen))
		}
		return cloned
	case reflect.Struct:
		cloned := reflect.New(value.Type()).Elem()
		cloned.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath == "" {
				cloned.Field(index).Set(cloneTaskData(value.Field(index), seen))
			}
		}
		return cloned
	default:
		return value
	}
}

func cloneTaskBase(base TaskStateBase) TaskStateBase {
	if base.EndTime != nil {
		value := *base.EndTime
		base.EndTime = &value
	}
	return base
}

// =============================================================================
// Task Creation Helpers
// =============================================================================

// CreateLocalAgentTask creates a new local agent task.
func CreateLocalAgentTask(id, prompt, agentType, description string) *LocalAgentTaskState {
	return &LocalAgentTaskState{
		TaskStateBase: TaskStateBase{
			ID:          id,
			Type:        TaskTypeLocalAgent,
			Status:      TaskStatusPending,
			Description: description,
			StartTime:   time.Now(),
			Notified:    false,
		},
		AgentID:         id,
		Prompt:          prompt,
		AgentType:       agentType,
		Retrieved:       false,
		IsBackgrounded:  false,
		PendingMessages: []string{},
		Retain:          false,
		DiskLoaded:      false,
	}
}

// CreateLocalShellTask creates a new local shell task.
func CreateLocalShellTask(id, command, directory, description string) *LocalShellTaskState {
	return &LocalShellTaskState{
		TaskStateBase: TaskStateBase{
			ID:          id,
			Type:        TaskTypeLocalShell,
			Status:      TaskStatusPending,
			Description: description,
			StartTime:   time.Now(),
			Notified:    false,
		},
		Command:        command,
		Directory:      directory,
		IsBackgrounded: false,
	}
}

// CreateRemoteAgentTask creates a new remote agent task.
func CreateRemoteAgentTask(id, command, sessionID, description string) *RemoteAgentTaskState {
	return &RemoteAgentTaskState{
		TaskStateBase: TaskStateBase{
			ID:          id,
			Type:        TaskTypeRemoteAgent,
			Status:      TaskStatusPending,
			Description: description,
			StartTime:   time.Now(),
			Notified:    false,
		},
		Command:        command,
		SessionID:      sessionID,
		IsBackgrounded: true, // Remote tasks are always backgrounded
	}
}

// =============================================================================
// Task Output Management
// =============================================================================

// GetTaskOutputPath returns the path to a task's output file.
func GetTaskOutputPath(taskID string) string {
	// Use system temp directory
	return filepath.Join(os.TempDir(), "claude-code-go", "tasks", taskID+".output")
}

// InitTaskOutput initializes the task output file.
func InitTaskOutput(taskID string) error {
	outputPath := GetTaskOutputPath(taskID)
	dir := filepath.Dir(outputPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create task output directory: %w", err)
	}

	// Create empty file
	if err := ioutil.WriteFile(outputPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create task output file: %w", err)
	}

	return nil
}

// AppendTaskOutput appends data to the task output file.
func AppendTaskOutput(taskID string, data []byte) error {
	outputPath := GetTaskOutputPath(taskID)

	f, err := os.OpenFile(outputPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open task output file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write task output: %w", err)
	}

	return nil
}

// ReadTaskOutput reads the task output file.
func ReadTaskOutput(taskID string) (string, error) {
	outputPath := GetTaskOutputPath(taskID)

	data, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to read task output: %w", err)
	}

	return string(data), nil
}

// EvictTaskOutput removes the task output file.
func EvictTaskOutput(taskID string) error {
	outputPath := GetTaskOutputPath(taskID)

	if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to evict task output: %w", err)
	}

	return nil
}

// =============================================================================
// Task Notification
// =============================================================================

// TaskNotification represents a notification about a task.
type TaskNotification struct {
	TaskID      string    `json:"taskId"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	OutputPath  string    `json:"outputPath"`
	Timestamp   time.Time `json:"timestamp"`
}

// CreateNotification creates a task notification.
func CreateNotification(task TaskState, errMsg string) TaskNotification {
	base := task.GetBase()
	return TaskNotification{
		TaskID:      base.ID,
		Description: base.Description,
		Status:      string(base.Status),
		Error:       errMsg,
		OutputPath:  GetTaskOutputPath(base.ID),
		Timestamp:   time.Now(),
	}
}

// ToJSON converts the notification to JSON.
func (n TaskNotification) ToJSON() (string, error) {
	data, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// =============================================================================
// Task State Transitions
// =============================================================================

// StartTask marks a task as running.
func StartTask(task TaskState) TaskState {
	base := task.GetBase()
	base.Status = TaskStatusRunning
	return task
}

// CompleteTask marks a task as completed.
func CompleteTask(task TaskState) TaskState {
	base := task.GetBase()
	base.Status = TaskStatusCompleted
	now := time.Now()
	base.EndTime = &now
	return task
}

// FailTask marks a task as failed.
func FailTask(task TaskState, errMsg string) TaskState {
	base := task.GetBase()
	base.Status = TaskStatusFailed
	now := time.Now()
	base.EndTime = &now

	// Set error message on specific task type
	switch t := task.(type) {
	case *LocalAgentTaskState:
		t.Error = errMsg
	case *LocalShellTaskState:
		t.Error = errMsg
	case *RemoteAgentTaskState:
		t.Error = errMsg
	}

	return task
}

// KillTask marks a task as killed.
func KillTask(task TaskState) TaskState {
	base := task.GetBase()
	base.Status = TaskStatusKilled
	now := time.Now()
	base.EndTime = &now
	return task
}
