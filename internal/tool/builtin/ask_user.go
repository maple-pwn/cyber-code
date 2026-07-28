package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

var ErrInteractiveInputUnavailable = errors.New("interactive user input is unavailable")

type Question struct {
	Prompt  string
	Options []string
}

type Questioner func(context.Context, Question) (string, error)

type askUserTool struct {
	questioner Questioner
	spec       tool.Spec
}

func NewAskUser(questioner Questioner) tool.Tool {
	return &askUserTool{questioner: questioner, spec: tool.Spec{
		Name: "ask_user", Description: "Ask the user one necessary question and wait for the answer",
		Schema: json.RawMessage(`{"type":"object","required":["question"],"properties":{"question":{"type":"string"},"options":{"type":"array"}},"additionalProperties":false}`),
	}}
}

func (ask *askUserTool) Spec() tool.Spec { return ask.spec }

func (ask *askUserTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	if _, err := parseQuestion(arguments); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: ask.spec.Name, Action: permissions.ActionRead}, nil
}

func (ask *askUserTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	question, err := parseQuestion(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	if ask.questioner == nil {
		return core.ToolResult{}, ErrInteractiveInputUnavailable
	}
	answer, err := ask.questioner(ctx, question)
	if err != nil {
		return core.ToolResult{}, err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return core.ToolResult{}, fmt.Errorf("user canceled the question")
	}
	return textResult("User answer: " + answer), nil
}

func parseQuestion(arguments json.RawMessage) (Question, error) {
	var input struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return Question{}, err
	}
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" || len(input.Question) > 4096 {
		return Question{}, fmt.Errorf("question must contain 1 to 4096 characters")
	}
	if len(input.Options) > 20 {
		return Question{}, fmt.Errorf("question supports at most 20 options")
	}
	seen := make(map[string]struct{}, len(input.Options))
	for index := range input.Options {
		input.Options[index] = strings.TrimSpace(input.Options[index])
		if input.Options[index] == "" || len(input.Options[index]) > 256 {
			return Question{}, fmt.Errorf("question option %d is invalid", index)
		}
		if _, exists := seen[input.Options[index]]; exists {
			return Question{}, fmt.Errorf("question option %q is duplicated", input.Options[index])
		}
		seen[input.Options[index]] = struct{}{}
	}
	return Question{Prompt: input.Question, Options: append([]string(nil), input.Options...)}, nil
}
