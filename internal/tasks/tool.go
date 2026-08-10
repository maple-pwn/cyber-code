package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cyber-code/internal/collaboration"
	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

type AgentRequest struct {
	TaskID      string
	Prompt      string
	Description string
	MaxTurns    int
	Mode        permissions.PermissionMode
	Definition  *collaboration.Definition
	Emit        func(core.Event) error
}

type AgentExecuteFunc func(context.Context, AgentRequest) (any, error)

type ToolServiceOptions struct {
	Manager        *Manager
	Queue          *Queue
	Execute        AgentExecuteFunc
	ParentMode     permissions.PermissionMode
	ParentMaxTurns int
	Definitions    []collaboration.Definition
	Board          *collaboration.Board
}

type ToolService struct {
	manager        *Manager
	execute        AgentExecuteFunc
	parentMode     permissions.PermissionMode
	parentMaxTurns int
	definitions    map[string]collaboration.Definition
	board          *collaboration.Board
	ctx            context.Context
	cancel         context.CancelFunc
	observations   *observationHub
	queue          *Queue
	queueWorker    *QueueWorker
}

type taskRunInput struct {
	Agent          string                     `json:"agent"`
	Prompt         string                     `json:"prompt"`
	Description    string                     `json:"description"`
	MaxTurns       int                        `json:"max_turns"`
	PermissionMode permissions.PermissionMode `json:"permission_mode"`
	Background     bool                       `json:"background"`
}

type taskIDInput struct {
	ID string `json:"id"`
}

type taskToolResult struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
	Result any        `json:"result,omitempty"`
	Error  string     `json:"error,omitempty"`
}

func NewToolService(options ToolServiceOptions) (*ToolService, error) {
	if options.Execute == nil {
		return nil, fmt.Errorf("task agent executor is required")
	}
	if options.Manager == nil {
		options.Manager = NewManager(nil)
	}
	parentMode := normalizedMode(options.ParentMode)
	if modePrivilege(parentMode) > modePrivilege(permissions.PermissionModeBypass) {
		return nil, fmt.Errorf("unsupported parent permission mode %q", parentMode)
	}
	ctx, cancel := context.WithCancel(context.Background())
	definitions := make(map[string]collaboration.Definition, len(options.Definitions))
	for _, definition := range options.Definitions {
		if _, exists := definitions[definition.Name]; exists {
			cancel()
			return nil, fmt.Errorf("agent definition %q is duplicated", definition.Name)
		}
		definition.Tools = append([]string(nil), definition.Tools...)
		definitions[definition.Name] = definition
	}
	service := &ToolService{
		manager: options.Manager, execute: options.Execute, parentMode: parentMode,
		parentMaxTurns: normalizedTurns(options.ParentMaxTurns), definitions: definitions, board: options.Board, ctx: ctx, cancel: cancel,
		observations: newObservationHub(defaultObservationBuffer), queue: options.Queue,
	}
	if options.Queue != nil {
		workerID, err := newQueueID()
		if err != nil {
			cancel()
			return nil, err
		}
		service.queueWorker, err = NewQueueWorker(options.Queue, QueueWorkerOptions{ID: "task-service-" + workerID}, service.executeQueued)
		if err != nil {
			cancel()
			return nil, err
		}
		service.queueWorker.Start(ctx)
	}
	return service, nil
}

func (service *ToolService) Close() error {
	if service == nil {
		return nil
	}
	service.cancel()
	if service.queueWorker != nil {
		_ = service.queueWorker.Drain(context.Background())
	}
	err := service.manager.Close()
	if service.queue != nil {
		err = errors.Join(err, service.queue.Close())
	}
	service.observations.close()
	return err
}

// Drain stops claiming new durable work and waits for the currently leased
// task to reach a terminal state. Pending work remains on disk for restart.
func (service *ToolService) Drain(ctx context.Context) error {
	if service == nil || service.queueWorker == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return service.queueWorker.Drain(ctx)
}

// Observe subscribes to bounded, provider-independent child task events.
func (service *ToolService) Observe(ctx context.Context) <-chan core.Event {
	if service == nil || service.observations == nil {
		closed := make(chan core.Event)
		close(closed)
		return closed
	}
	return service.observations.observe(ctx)
}

// Snapshots returns immutable task observation summaries ordered by task ID.
func (service *ToolService) Snapshots() []Snapshot {
	if service == nil || service.observations == nil {
		return nil
	}
	return service.observations.snapshots()
}

func RegisterTools(registry *toolpkg.Registry, service *ToolService) error {
	if registry == nil || service == nil {
		return fmt.Errorf("task tool registry and service are required")
	}
	for _, model := range []*taskTool{
		{name: "task_run", description: "Run a bounded sub-agent task", service: service, schema: json.RawMessage(`{"type":"object","required":["prompt"],"properties":{"agent":{"type":"string"},"prompt":{"type":"string"},"description":{"type":"string"},"max_turns":{"type":"integer"},"permission_mode":{"type":"string"},"background":{"type":"boolean"}},"additionalProperties":false}`)},
		{name: "task_status", description: "Read a sub-agent task status and result", service: service, readOnly: true, schema: taskIDSchema()},
		{name: "task_cancel", description: "Cancel a running sub-agent task", service: service, schema: taskIDSchema()},
	} {
		if err := registry.Register(model); err != nil {
			return err
		}
	}
	return nil
}

func taskIDSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}},"additionalProperties":false}`)
}

type taskTool struct {
	name, description string
	schema            json.RawMessage
	readOnly          bool
	service           *ToolService
}

func (model *taskTool) Spec() toolpkg.Spec {
	return toolpkg.Spec{Name: model.name, Description: model.description, Schema: model.schema, ReadOnly: model.readOnly}
}

func (model *taskTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	if model.name == "task_run" {
		if _, err := model.service.parseRun(arguments); err != nil {
			return permissions.Request{}, err
		}
		return permissions.Request{Tool: model.name, Action: permissions.ActionExecute, Command: "sub-agent"}, nil
	}
	input, err := parseTaskID(arguments)
	if err != nil {
		return permissions.Request{}, err
	}
	if model.service.manager.GetTask(input.ID) == nil {
		return permissions.Request{}, fmt.Errorf("task %q not found", input.ID)
	}
	action := permissions.ActionExecute
	if model.readOnly {
		action = permissions.ActionRead
	}
	return permissions.Request{Tool: model.name, Action: action}, nil
}

func (model *taskTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var result taskToolResult
	var err error
	switch model.name {
	case "task_run":
		result, err = model.service.run(ctx, arguments)
	case "task_status":
		var input taskIDInput
		input, err = parseTaskID(arguments)
		if err == nil {
			result, err = model.service.result(input.ID)
		}
	case "task_cancel":
		var input taskIDInput
		input, err = parseTaskID(arguments)
		if err == nil {
			err = model.service.cancelTask(ctx, input.ID)
		}
		if err == nil {
			result, err = model.service.result(input.ID)
		}
	default:
		err = fmt.Errorf("unsupported task tool %q", model.name)
	}
	if err != nil {
		return core.ToolResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return core.ToolResult{}, err
	}
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: string(encoded)}}}, nil
}

func (service *ToolService) cancelTask(ctx context.Context, id string) error {
	state := service.manager.GetTask(id)
	if state == nil {
		return fmt.Errorf("task %q not found", id)
	}
	err := service.manager.KillTask(id)
	if err != nil {
		if service.queue == nil || state.GetBase().Status != TaskStatusPending {
			return err
		}
		if _, queueErr := service.queue.CancelByKey(ctx, id); queueErr != nil {
			return queueErr
		}
		service.observations.recordQueue(id, QueueCancelled, 0)
		if transitionErr := service.manager.registry.Transition(id, TaskStatusCancelled, context.Canceled); transitionErr != nil {
			return transitionErr
		}
		_ = service.publishStatus(core.EventSubagentStatus, id, "", state.GetBase().Description, TaskStatusCancelled)
	} else if service.queue != nil {
		if _, queueErr := service.queue.CancelByKey(ctx, id); queueErr != nil && !errors.Is(queueErr, ErrQueueItem) {
			return queueErr
		}
		service.observations.recordQueue(id, QueueCancelled, 0)
	}
	if service.board != nil {
		return service.board.Transition(ctx, id, collaboration.TaskCancelled, "cancelled by parent")
	}
	return nil
}

func (service *ToolService) parseRun(arguments json.RawMessage) (taskRunInput, error) {
	var input taskRunInput
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Agent = strings.TrimSpace(input.Agent)
	input.Description = strings.TrimSpace(input.Description)
	if input.Prompt == "" {
		return input, fmt.Errorf("task prompt is required")
	}
	if input.Description == "" {
		input.Description = input.Prompt
	}
	turnLimit := service.parentMaxTurns
	modeLimit := service.parentMode
	if input.Agent != "" {
		definition, exists := service.definitions[input.Agent]
		if !exists {
			return input, fmt.Errorf("agent definition %q was not found", input.Agent)
		}
		if definition.MaxTurns < turnLimit {
			turnLimit = definition.MaxTurns
		}
		if modePrivilege(definition.PermissionMode) < modePrivilege(modeLimit) {
			modeLimit = definition.PermissionMode
		}
	}
	if input.MaxTurns == 0 {
		input.MaxTurns = turnLimit
	}
	if input.MaxTurns < 1 || input.MaxTurns > turnLimit {
		return input, fmt.Errorf("%w: child %d, limit %d", ErrBudgetExceeded, input.MaxTurns, turnLimit)
	}
	if input.PermissionMode == "" {
		input.PermissionMode = modeLimit
	}
	if modePrivilege(input.PermissionMode) > modePrivilege(modeLimit) {
		return input, fmt.Errorf("%w: child %s, limit %s", ErrPermissionEscalation, input.PermissionMode, modeLimit)
	}
	return input, nil
}

func (service *ToolService) run(ctx context.Context, arguments json.RawMessage) (taskToolResult, error) {
	input, err := service.parseRun(arguments)
	if err != nil {
		return taskToolResult{}, err
	}
	task, err := service.manager.SpawnLocalAgent(ctx, input.Prompt, "sub-agent", input.Description, input.Background)
	if err != nil {
		return taskToolResult{}, err
	}
	if err := service.publishStatus(core.EventSubagentStarted, task.ID, input.Agent, input.Description, TaskStatusPending); err != nil {
		service.manager.registry.Unregister(task.ID)
		return taskToolResult{}, err
	}
	if service.board != nil {
		if err := service.board.Create(ctx, collaboration.Task{ID: task.ID, Agent: input.Agent, Description: input.Description}); err != nil {
			service.manager.registry.Unregister(task.ID)
			return taskToolResult{}, err
		}
	}
	request := service.agentRequest(task, input)
	if input.Background && service.queue != nil {
		payload, encodeErr := json.Marshal(queuedAgentTask{TaskID: task.ID, Input: input})
		if encodeErr != nil {
			service.manager.registry.Unregister(task.ID)
			return taskToolResult{}, encodeErr
		}
		if _, _, enqueueErr := service.queue.Enqueue(ctx, task.ID, payload); enqueueErr != nil {
			service.manager.registry.Unregister(task.ID)
			if service.board != nil {
				_ = service.board.Transition(context.Background(), task.ID, collaboration.TaskCancelled, enqueueErr.Error())
			}
			_ = service.publishStatus(core.EventSubagentStatus, task.ID, input.Agent, input.Description, TaskStatusCancelled)
			return taskToolResult{}, enqueueErr
		}
		service.observations.recordQueue(task.ID, QueuePending, 0)
		return service.result(task.ID)
	}
	executionCtx := ctx
	if input.Background {
		executionCtx = service.ctx
	}
	if err := service.startExecution(executionCtx, task, input, request); err != nil {
		if service.board != nil {
			_ = service.board.Transition(context.Background(), task.ID, collaboration.TaskCancelled, err.Error())
		}
		_ = service.publishStatus(core.EventSubagentStatus, task.ID, input.Agent, input.Description, TaskStatusCancelled)
		return taskToolResult{}, err
	}
	if input.Background {
		return service.result(task.ID)
	}
	return service.wait(ctx, task.ID)
}

type queuedAgentTask struct {
	TaskID string       `json:"task_id"`
	Input  taskRunInput `json:"input"`
}

func (service *ToolService) executeQueued(ctx context.Context, item QueueItem) error {
	var queued queuedAgentTask
	if err := json.Unmarshal(item.Payload, &queued); err != nil {
		return fmt.Errorf("decode queued task: %w", err)
	}
	if strings.TrimSpace(queued.TaskID) == "" || queued.Input.Prompt == "" || !queued.Input.Background {
		return fmt.Errorf("queued task payload is invalid")
	}
	service.observations.recordQueue(queued.TaskID, QueueLeased, item.Attempts)
	state := service.manager.GetTask(queued.TaskID)
	if state == nil {
		task := CreateLocalAgentTask(queued.TaskID, queued.Input.Prompt, "sub-agent", queued.Input.Description)
		task.IsBackgrounded = true
		if err := service.manager.registry.Register(task); err != nil {
			return err
		}
		service.manager.notifyTaskCreated(task)
		if err := service.publishStatus(core.EventSubagentStarted, task.ID, queued.Input.Agent, queued.Input.Description, TaskStatusPending); err != nil {
			return err
		}
		if service.board != nil {
			existing, exists, err := service.board.Get(ctx, task.ID)
			if err != nil {
				return err
			}
			if !exists {
				if err := service.board.Create(ctx, collaboration.Task{ID: task.ID, Agent: queued.Input.Agent, Description: queued.Input.Description}); err != nil {
					return err
				}
			} else if existing.Status == collaboration.TaskFailed && existing.Error == "interrupted by process restart" {
				if err := service.board.RetryInterrupted(ctx, task.ID); err != nil {
					return err
				}
			}
		}
		state = task
	}
	status := state.GetBase().Status
	if status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled {
		queueStatus := QueueCompleted
		if status == TaskStatusCancelled {
			queueStatus = QueueCancelled
		}
		service.observations.recordQueue(queued.TaskID, queueStatus, item.Attempts)
		return nil
	}
	if status == TaskStatusRunning {
		_, err := service.wait(ctx, queued.TaskID)
		return err
	}
	task, ok := state.(*LocalAgentTaskState)
	if !ok {
		return fmt.Errorf("queued task %q is not a local agent task", queued.TaskID)
	}
	request := service.agentRequest(task, queued.Input)
	if err := service.startExecution(ctx, task, queued.Input, request); err != nil {
		return err
	}
	result, err := service.wait(ctx, queued.TaskID)
	if err == nil {
		queueStatus := QueueCompleted
		if result.Status == TaskStatusCancelled {
			queueStatus = QueueCancelled
		}
		service.observations.recordQueue(queued.TaskID, queueStatus, item.Attempts)
	}
	return err
}

func (service *ToolService) agentRequest(task *LocalAgentTaskState, input taskRunInput) AgentRequest {
	request := AgentRequest{TaskID: task.ID, Prompt: input.Prompt, Description: input.Description, MaxTurns: input.MaxTurns, Mode: input.PermissionMode}
	request.Emit = func(event core.Event) error {
		return service.observations.publish(core.Event{
			Type: core.EventSubagentEvent,
			Subagent: &core.SubagentEvent{
				TaskID: task.ID, Agent: input.Agent, Description: input.Description, Status: string(TaskStatusRunning), Event: &event,
			},
		})
	}
	if input.Agent != "" {
		definition := service.definitions[input.Agent]
		definition.Tools = append([]string(nil), definition.Tools...)
		request.Definition = &definition
	}
	return request
}

func (service *ToolService) startExecution(executionCtx context.Context, task *LocalAgentTaskState, input taskRunInput, request AgentRequest) error {
	return service.manager.StartExecution(executionCtx, task.ID, func(ctx context.Context, _ *LocalAgentTaskState) (any, error) {
		if err := service.publishStatus(core.EventSubagentStatus, request.TaskID, input.Agent, input.Description, TaskStatusRunning); err != nil {
			return nil, err
		}
		if service.board != nil {
			if err := service.board.Transition(ctx, request.TaskID, collaboration.TaskRunning, ""); err != nil {
				return nil, err
			}
		}
		result, executeErr := service.execute(ctx, request)
		if service.board != nil {
			status, message := collaboration.TaskCompleted, ""
			if executeErr != nil {
				status, message = collaboration.TaskFailed, executeErr.Error()
				if errors.Is(executeErr, context.Canceled) {
					status = collaboration.TaskCancelled
				}
			}
			if transitionErr := service.board.Transition(context.Background(), request.TaskID, status, message); transitionErr != nil && executeErr == nil {
				executeErr = transitionErr
			}
		}
		terminalStatus := TaskStatusCompleted
		if executeErr != nil {
			terminalStatus = TaskStatusFailed
			if errors.Is(executeErr, context.Canceled) {
				terminalStatus = TaskStatusCancelled
			}
		}
		if publishErr := service.publishStatus(core.EventSubagentStatus, request.TaskID, input.Agent, input.Description, terminalStatus); publishErr != nil && executeErr == nil {
			executeErr = publishErr
		}
		if input.Background && service.queue != nil {
			queueStatus := QueueCompleted
			if terminalStatus == TaskStatusCancelled {
				queueStatus = QueueCancelled
			}
			service.observations.recordQueue(request.TaskID, queueStatus, 0)
		}
		return result, executeErr
	})
}

func (service *ToolService) publishStatus(eventType core.EventType, taskID, agent, description string, status TaskStatus) error {
	return service.observations.publish(core.Event{
		Type: eventType,
		Subagent: &core.SubagentEvent{
			TaskID: taskID, Agent: agent, Description: description, Status: string(status),
		},
	})
}

func (service *ToolService) wait(ctx context.Context, id string) (taskToolResult, error) {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := service.result(id)
		if err != nil || result.Status == TaskStatusCompleted || result.Status == TaskStatusFailed || result.Status == TaskStatusCancelled {
			return result, err
		}
		select {
		case <-ctx.Done():
			return taskToolResult{}, ctx.Err()
		case <-service.ctx.Done():
			return taskToolResult{}, context.Canceled
		case <-ticker.C:
		}
	}
}

func (service *ToolService) result(id string) (taskToolResult, error) {
	state := service.manager.GetTask(id)
	if state == nil {
		return taskToolResult{}, fmt.Errorf("task %q not found", id)
	}
	result := taskToolResult{ID: id, Status: state.GetBase().Status}
	if agentTask, ok := state.(*LocalAgentTaskState); ok {
		result.Result = agentTask.Result
		result.Error = agentTask.Error
	}
	return result, nil
}

func parseTaskID(arguments json.RawMessage) (taskIDInput, error) {
	var input taskIDInput
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		return input, fmt.Errorf("task ID is required")
	}
	return input, nil
}
