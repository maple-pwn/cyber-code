package services

import (
	"context"
	"testing"

	"claude-code-go/internal/platform"
)

func TestNotifyServiceUsesAvailableNativeAdapter(t *testing.T) {
	native := &nativeNotificationStub{capability: platform.Capability{Available: true, Backend: "test"}}
	if err := SendNotificationWithNative(context.Background(), NotificationOptions{Title: "title", Message: "message"}, nil, "native", native); err != nil {
		t.Fatal(err)
	}
	if native.title != "title" || native.message != "message" {
		t.Fatalf("title = %q, message = %q", native.title, native.message)
	}
}

type nativeNotificationStub struct {
	capability     platform.Capability
	title, message string
}

func (stub *nativeNotificationStub) Capability() platform.Capability { return stub.capability }
func (stub *nativeNotificationStub) Notify(_ context.Context, title, message string) error {
	stub.title, stub.message = title, message
	return nil
}
