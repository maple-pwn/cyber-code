package services

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewCompactServiceUsesDefaultTokenEstimator(t *testing.T) {
	service := NewCompactService()
	result, err := service.CompactConversation(context.Background(), []*CompactMessage{{
		Role: "user", Content: json.RawMessage(`"hello"`), UUID: "message-1",
	}}, "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreCompactTokenCount <= 0 || result.PostCompactTokenCount <= 0 {
		t.Fatalf("compact token counts = %#v", result)
	}
}
