package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
}

type AgentExecuteFunc func(context.Context, AgentRequest) (any, error)

type ToolServiceOptions struct {
	Manager        *Manager
	Execute        AgentExecuteFunc
	ParentMode     permissions.PermissionMode
	ParentMaxTurns int
}

type ToolService struct {
	manager        *Manager
	execute        AgentExecuteFunc
	parentMode     permissions.PermissionMode
	parentMaxTurns int
	ctx            context.Context
	cancel         context.CancelFunc
}

type taskRunInput struct {
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
	return &ToolService{
		manager: options.Manager, execute: options.Execute, parentMode: parentMode,
		parentMaxTurns: normalizedTurns(options.ParentMaxTurns), ctx: ctx, cancel: cancel,
	}, nil
}

func (service *ToolService) Close() error {
	if service == nil {
		return nil
	}
	service.cancel()
	return service.manager.Close()
}

func RegisterTools(registry *toolpkg.Registry, service *ToolService) error {
	if registry == nil || service == nil {
		return fmt.Errorf("task tool registry and service are required")
	}
	for _, model := range []*taskTool{
		{name: "task_run", description: "Run a bounded sub-agent task", service: service, schema: json.RawMessage(`{"type":"object","required":["prompt"],"properties":{"prompt":{"type":"string"},"description":{"type":"string"},"max_turns":{"type":"integer"},"permission_mode":{"type":"string"},"background":{"type":"boolean"}},"additionalProperties":false}`)},
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
			err = model.service.manager.KillTask(input.ID)
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

func (service *ToolService) parseRun(arguments json.RawMessage) (taskRunInput, error) {
	var input taskRunInput
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Description = strings.TrimSpace(input.Description)
	if input.Prompt == "" {
		return input, fmt.Errorf("task prompt is required")
	}
	if input.Description == "" {
		input.Description = input.Prompt
	}
	if input.MaxTurns == 0 {
		input.MaxTurns = service.parentMaxTurns
	}
	if input.MaxTurns < 1 || input.MaxTurns > service.parentMaxTurns {
		return input, fmt.Errorf("%w: child %d, parent %d", ErrBudgetExceeded, input.MaxTurns, service.parentMaxTurns)
	}
	if input.PermissionMode == "" {
		input.PermissionMode = service.parentMode
	}
	if modePrivilege(input.PermissionMode) > modePrivilege(service.parentMode) {
		return input, fmt.Errorf("%w: child %s, parent %s", ErrPermissionEscalation, input.PermissionMode, service.parentMode)
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
	request := AgentRequest{TaskID: task.ID, Prompt: input.Prompt, Description: input.Description, MaxTurns: input.MaxTurns, Mode: input.PermissionMode}
	executionCtx := ctx
	if input.Background {
		executionCtx = service.ctx
	}
	if err := service.manager.StartExecution(executionCtx, task.ID, func(ctx context.Context, _ *LocalAgentTaskState) (any, error) {
		return service.execute(ctx, request)
	}); err != nil {
		return taskToolResult{}, err
	}
	if input.Background {
		return service.result(task.ID)
	}
	return service.wait(ctx, task.ID)
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
