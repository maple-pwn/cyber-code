package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestBrokerModeMatrix(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name   string
		mode   PermissionMode
		action string
		want   PermissionBehavior
	}{
		{name: "default read", mode: PermissionModeDefault, action: ActionRead, want: PermissionBehaviorAllow},
		{name: "default write headless", mode: PermissionModeDefault, action: ActionWrite, want: PermissionBehaviorDeny},
		{name: "plan read", mode: PermissionModePlan, action: ActionRead, want: PermissionBehaviorAllow},
		{name: "plan write", mode: PermissionModePlan, action: ActionWrite, want: PermissionBehaviorDeny},
		{name: "accept edits write", mode: PermissionModeAcceptEdits, action: ActionWrite, want: PermissionBehaviorAllow},
		{name: "accept edits execute headless", mode: PermissionModeAcceptEdits, action: ActionExecute, want: PermissionBehaviorDeny},
		{name: "bypass execute", mode: PermissionModeBypass, action: ActionExecute, want: PermissionBehaviorAllow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, err := NewBroker(Options{Mode: test.mode, ModeSource: SourceCliArg, Headless: true})
			if err != nil {
				t.Fatal(err)
			}
			decision, err := broker.Decide(context.Background(), Request{
				Tool: "tool", Action: test.action, Workspace: workspace, Paths: []string{filepath.Join(workspace, "file.txt")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Behavior != test.want {
				t.Fatalf("behavior = %q, want %q (%s)", decision.Behavior, test.want, decision.Reason)
			}
		})
	}
}

func TestBrokerDenyPrecedesExplicitAllow(t *testing.T) {
	broker, err := NewBroker(Options{Mode: PermissionModeDefault, Rules: []Rule{
		{ID: "allow-shell", Source: SourceUserSettings, Behavior: PermissionBehaviorAllow, Tool: "shell", Action: ActionExecute},
		{ID: "deny-shell", Source: SourceProjectSettings, Behavior: PermissionBehaviorDeny, Tool: "shell", Action: ActionExecute},
	}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := broker.Decide(context.Background(), Request{Tool: "shell", Action: ActionExecute, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != PermissionBehaviorDeny || decision.RuleID != "deny-shell" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestBrokerRejectsProjectEscalation(t *testing.T) {
	_, err := NewBroker(Options{Mode: PermissionModeBypass, ModeSource: SourceProjectSettings})
	if err == nil {
		t.Fatal("project settings enabled bypass mode")
	}
	_, err = NewBroker(Options{Rules: []Rule{{
		ID: "project-allow", Source: SourceProjectSettings, Behavior: PermissionBehaviorAllow, Tool: "shell",
	}}})
	if err == nil {
		t.Fatal("project settings added an allow rule")
	}
}

func TestBrokerHeadlessAskDeniesAndInteractiveConfirmerCanAllow(t *testing.T) {
	request := Request{Tool: "shell", Action: ActionExecute, Workspace: t.TempDir()}
	headless, err := NewBroker(Options{Mode: PermissionModeDefault, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := headless.Decide(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != PermissionBehaviorDeny {
		t.Fatalf("headless decision = %#v", decision)
	}

	interactive, err := NewBroker(Options{Mode: PermissionModeDefault, Confirmer: func(context.Context, Request) (Decision, error) {
		return Decision{Behavior: PermissionBehaviorAllow}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err = interactive.Decide(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != PermissionBehaviorAllow {
		t.Fatalf("interactive decision = %#v", decision)
	}
}

func TestBrokerDeniesTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(Options{Mode: PermissionModeBypass, ModeSource: SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(workspace, "..", "outside", "secret.txt"), outside} {
		decision, err := broker.Decide(context.Background(), Request{Tool: "write", Action: ActionWrite, Workspace: workspace, Paths: []string{path}})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Behavior != PermissionBehaviorDeny {
			t.Fatalf("escaped path %q was allowed: %#v", path, decision)
		}
	}

	link := filepath.Join(workspace, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	decision, err := broker.Decide(context.Background(), Request{Tool: "write", Action: ActionWrite, Workspace: workspace, Paths: []string{filepath.Join(link, "secret.txt")}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != PermissionBehaviorDeny {
		t.Fatalf("symlink escape was allowed: %#v", decision)
	}
}

func TestChildBrokerCannotElevateParent(t *testing.T) {
	parent, err := NewBroker(Options{Mode: PermissionModePlan})
	if err != nil {
		t.Fatal(err)
	}
	child, err := parent.Child(PermissionModeBypass, SourceCliArg, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := child.Decide(context.Background(), Request{Tool: "write", Action: ActionWrite, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != PermissionBehaviorDeny {
		t.Fatalf("child elevated parent permission: %#v", decision)
	}
}

func TestAuditDoesNotStoreCommandOrSecrets(t *testing.T) {
	audit := NewAuditLog(10)
	broker, err := NewBroker(Options{Mode: PermissionModeDefault, Headless: true, Audit: audit})
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-super-secret"
	fileContents := "private file contents"
	_, err = broker.Decide(context.Background(), Request{
		Tool: "shell", Action: ActionExecute, Workspace: t.TempDir(),
		Command: "curl -H 'Authorization: Bearer " + secret + "' --data '" + fileContents + "'",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(audit.Records())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, secret) || strings.Contains(text, fileContents) || strings.Contains(strings.ToLower(text), "authorization") {
		t.Fatalf("audit leaked request data: %s", text)
	}
}

func TestBrokerDeniesMalformedRequestsEvenInBypassMode(t *testing.T) {
	broker, err := NewBroker(Options{Mode: PermissionModeBypass, ModeSource: SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{
		{Action: ActionRead, Workspace: t.TempDir()},
		{Tool: "unknown", Action: "teleport", Workspace: t.TempDir()},
		{Tool: "shell", Action: ActionRead, Workspace: t.TempDir(), Command: "remove files"},
		{Tool: "http", Action: ActionRead, Workspace: t.TempDir(), Network: []string{"example.com"}},
	} {
		decision, err := broker.Decide(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Behavior != PermissionBehaviorDeny {
			t.Fatalf("malformed request was allowed: %#v", decision)
		}
	}
}

func TestManagerDenyRulesPrecedeAllAllowRules(t *testing.T) {
	manager := NewManager()
	manager.AddRule(SourcePolicySettings, PermissionRule{
		Source: SourcePolicySettings, RuleBehavior: PermissionBehaviorAllow,
		RuleValue: PermissionRuleValue{ToolName: "shell"},
	})
	manager.AddRule(SourceUserSettings, PermissionRule{
		Source: SourceUserSettings, RuleBehavior: PermissionBehaviorDeny,
		RuleValue: PermissionRuleValue{ToolName: "shell"},
	})
	result := manager.CheckPermission(context.Background(), "shell", "")
	if result.Behavior != PermissionBehaviorDeny {
		t.Fatalf("result = %#v", result)
	}
}

func TestManagerConcurrentPatternChecks(t *testing.T) {
	manager := NewManager()
	const checks = 32
	for index := 0; index < checks; index++ {
		manager.AddRule(SourceUserSettings, PermissionRule{
			Source: SourceUserSettings, RuleBehavior: PermissionBehaviorAllow,
			RuleValue: PermissionRuleValue{ToolName: "shell", RuleContent: fmt.Sprintf("command-%d-*", index)},
		})
	}
	var wait sync.WaitGroup
	wait.Add(checks)
	for index := 0; index < checks; index++ {
		go func(index int) {
			defer wait.Done()
			result := manager.CheckPermission(context.Background(), "shell", fmt.Sprintf("command-%d-value", index))
			if result.Behavior != PermissionBehaviorAllow {
				t.Errorf("check %d = %#v", index, result)
			}
		}(index)
	}
	wait.Wait()
}
