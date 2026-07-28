package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAskUserReturnsInteractiveAnswer(t *testing.T) {
	tool := NewAskUser(func(_ context.Context, question Question) (string, error) {
		if question.Prompt != "Choose" || len(question.Options) != 2 {
			t.Fatalf("question = %#v", question)
		}
		return "second", nil
	})
	arguments, _ := json.Marshal(map[string]any{"question": "Choose", "options": []string{"first", "second"}})
	result, err := tool.Run(context.Background(), arguments)
	if err != nil || result.Content[0].Text != "User answer: second" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestAskUserHeadlessFailsWithoutBlocking(t *testing.T) {
	tool := NewAskUser(nil)
	_, err := tool.Run(context.Background(), json.RawMessage(`{"question":"Need input"}`))
	if !errors.Is(err, ErrInteractiveInputUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
