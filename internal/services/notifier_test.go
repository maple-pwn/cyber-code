package services

import (
	"context"
	"testing"

	"cyber-code/internal/platform"
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

func TestNotificationDefaultsToCyberCodeTitle(t *testing.T) {
	terminal := &terminalNotificationStub{}
	if got := sendToChannel("iterm2", NotificationOptions{Message: "done"}, terminal); got != "iterm2" {
		t.Fatalf("channel = %q", got)
	}
	if terminal.notification.Title != "cyber-code" {
		t.Fatalf("notification = %#v", terminal.notification)
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

type terminalNotificationStub struct {
	notification NotificationOptions
}

func (stub *terminalNotificationStub) NotifyITerm2(options NotificationOptions) {
	stub.notification = options
}
func (*terminalNotificationStub) NotifyKitty(KittyNotificationOptions) {}
func (*terminalNotificationStub) NotifyGhostty(NotificationOptions)    {}
func (*terminalNotificationStub) NotifyBell()                          {}
