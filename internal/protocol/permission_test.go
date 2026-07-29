package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"cyber-code/internal/permissions"
)

func TestPermissionBrokerRoutesOnlyMatchingResponse(t *testing.T) {
	broker := NewPermissionBroker(2)
	result := make(chan permissions.Decision, 1)
	go func() {
		decision, _ := broker.Confirm(context.Background(), permissions.Request{Tool: "shell", Action: permissions.ActionExecute})
		result <- decision
	}()
	request := <-broker.Requests()
	if request.ID == "" || request.Request.Tool != "shell" {
		t.Fatalf("request=%#v", request)
	}
	if err := broker.Respond("forged", permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}); !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("forged error=%v", err)
	}
	if err := broker.Respond(request.ID, permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}); err != nil {
		t.Fatal(err)
	}
	select {
	case decision := <-result:
		if decision.Behavior != permissions.PermissionBehaviorAllow {
			t.Fatalf("decision=%#v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("permission response timed out")
	}
}

func TestPermissionBrokerCloseDeniesPendingRequest(t *testing.T) {
	broker := NewPermissionBroker(1)
	result := make(chan permissions.Decision, 1)
	go func() {
		decision, _ := broker.Confirm(context.Background(), permissions.Request{Tool: "write"})
		result <- decision
	}()
	<-broker.Requests()
	broker.Close()
	if decision := <-result; decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestPermissionBrokerDisconnectDropsStalePromptsAndCanReconnect(t *testing.T) {
	broker := NewPermissionBroker(2)
	defer broker.Close()
	result := make(chan permissions.Decision, 1)
	go func() {
		decision, _ := broker.Confirm(context.Background(), permissions.Request{Tool: "old"})
		result <- decision
	}()
	deadline := time.Now().Add(time.Second)
	for len(broker.Requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	broker.Disconnect()
	if decision := <-result; decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("disconnect decision=%#v", decision)
	}
	if len(broker.Requests()) != 0 {
		t.Fatal("stale permission prompt survived disconnect")
	}
	go func() { _, _ = broker.Confirm(context.Background(), permissions.Request{Tool: "new"}) }()
	if prompt := <-broker.Requests(); prompt.Request.Tool != "new" {
		t.Fatalf("prompt=%#v", prompt)
	}
}
