package permissions

import (
	"context"
	"testing"
)

func TestManagedTrustBoundsToolsWithoutGrantingPermission(t *testing.T) {
	rules, err := TrustRules(TrustManaged, []string{"read_file", "shell"}, []string{"shell"})
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(Options{Mode: PermissionModeDefault, Headless: true, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		request Request
		want    PermissionBehavior
	}{
		{Request{Tool: "read_file", Action: ActionRead}, PermissionBehaviorAllow},
		{Request{Tool: "shell", Action: ActionExecute}, PermissionBehaviorDeny},
		{Request{Tool: "write_file", Action: ActionWrite}, PermissionBehaviorDeny},
	} {
		decision, err := broker.Decide(context.Background(), test.request)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Behavior != test.want {
			t.Fatalf("%s decision = %#v, want %s", test.request.Tool, decision, test.want)
		}
	}
}

func TestSuggestedTrustDenyListOnlyRestricts(t *testing.T) {
	rules, err := TrustRules(TrustSuggested, []string{"shell"}, []string{"delete_file"})
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(Options{Mode: PermissionModeDefault, Headless: true, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := broker.Decide(context.Background(), Request{Tool: "shell", Action: ActionExecute})
	if err != nil {
		t.Fatal(err)
	}
	// shell remains subject to default/headless confirmation; the allow list
	// does not grant execution.
	if decision.Behavior != PermissionBehaviorDeny || decision.RuleID != "" {
		t.Fatalf("suggested allow escalated permission: %#v", decision)
	}
}
