package permissions

import (
	"context"
	"encoding/json"
	"errors"
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

func TestBrokerFailsClosedWhenAuditCannotBeRecorded(t *testing.T) {
	broker, err := NewBroker(Options{Mode: PermissionModeDefault, Audit: failingAuditSink{}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := broker.Decide(context.Background(), Request{Tool: "read_file", Action: ActionRead})
	if err == nil || !strings.Contains(err.Error(), "audit") || decision.Behavior != "" {
		t.Fatalf("decision=%#v error=%v", decision, err)
	}
}

type failingAuditSink struct{}

func (failingAuditSink) Record(AuditRecord) error { return errors.New("disk unavailable") }

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

func TestPermissionResultHelpers(t *testing.T) {
	allow := &PermissionResult{Behavior: PermissionBehaviorAllow}
	deny := &PermissionResult{Behavior: PermissionBehaviorDeny}
	ask := &PermissionResult{Behavior: PermissionBehaviorAsk}
	passthrough := &PermissionResult{Behavior: PermissionBehaviorPassthrough}
	if !allow.IsAllowed() || allow.IsDenied() || allow.NeedsPrompt() {
		t.Fatalf("allow helpers are inconsistent: %#v", allow)
	}
	if !deny.IsDenied() || deny.IsAllowed() || deny.NeedsPrompt() {
		t.Fatalf("deny helpers are inconsistent: %#v", deny)
	}
	if !ask.NeedsPrompt() || !passthrough.NeedsPrompt() {
		t.Fatal("prompt behaviors were not recognized")
	}
}

func TestManagerRuleLifecycleAndWildcardMatching(t *testing.T) {
	manager := NewManager()
	wildcard := PermissionRule{
		Source: SourceUserSettings, RuleBehavior: PermissionBehaviorAllow,
		RuleValue: PermissionRuleValue{ToolName: "Bash(git:*)", RuleContent: "git status?.txt"},
	}
	manager.AddRule(SourceUserSettings, wildcard)
	rules := manager.GetRules(SourceUserSettings)
	if len(rules) != 1 {
		t.Fatalf("rules = %#v", rules)
	}
	rules[0].RuleValue.ToolName = "mutated"
	if manager.GetRules(SourceUserSettings)[0].RuleValue.ToolName != "Bash(git:*)" {
		t.Fatal("GetRules returned mutable manager storage")
	}
	if result := manager.CheckPermission(context.Background(), "Bash", "git status1.txt"); !result.IsAllowed() {
		t.Fatalf("wildcard result = %#v", result)
	}
	if result := manager.CheckPermission(context.Background(), "Bash", "git status.txt"); !result.NeedsPrompt() {
		t.Fatalf("non-matching result = %#v", result)
	}
	if result := manager.CheckPermission(context.Background(), "Other", "git status1.txt"); !result.NeedsPrompt() {
		t.Fatalf("tool mismatch result = %#v", result)
	}

	manager.RemoveRule(SourceUserSettings, wildcard.RuleValue)
	if len(manager.GetRules(SourceUserSettings)) != 0 {
		t.Fatal("RemoveRule did not remove matching rule")
	}
	manager.AddRule(SourceSession, PermissionRule{Source: SourceSession, RuleBehavior: PermissionBehaviorPassthrough, RuleValue: PermissionRuleValue{ToolName: "shell"}})
	if result := manager.CheckPermission(context.Background(), "shell", ""); result.Behavior != PermissionBehaviorPassthrough {
		t.Fatalf("passthrough result = %#v", result)
	}
	manager.ClearRules(SourceSession)
	if len(manager.GetRules(SourceSession)) != 0 {
		t.Fatal("ClearRules retained rules")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result := manager.CheckPermission(canceled, "shell", ""); !result.IsDenied() || !strings.Contains(result.Message, "canceled") {
		t.Fatalf("canceled result = %#v", result)
	}
}

func TestRuleValueModesAndUpdates(t *testing.T) {
	plain := ParseRuleValue("Read")
	if plain.ToolName != "Read" || plain.RuleContent != "" || FormatRuleValue(plain) != "Read" {
		t.Fatalf("plain rule = %#v", plain)
	}
	parsed := ParseRuleValue("Bash(git:*)")
	if parsed.ToolName != "Bash" || parsed.RuleContent != "git:*" || FormatRuleValue(parsed) != "Bash(git:*)" {
		t.Fatalf("parsed rule = %#v", parsed)
	}
	for _, mode := range []PermissionMode{PermissionModeAccept, PermissionModeAcceptEdits, PermissionModeBypass} {
		if got := GetModeBehavior(mode); got != PermissionBehaviorAllow {
			t.Errorf("GetModeBehavior(%q) = %q", mode, got)
		}
	}
	for _, mode := range []PermissionMode{PermissionModeDefault, PermissionModePlan, "unknown"} {
		if got := GetModeBehavior(mode); got != PermissionBehaviorAsk {
			t.Errorf("GetModeBehavior(%q) = %q", mode, got)
		}
	}

	manager := NewManager()
	read := PermissionRuleValue{ToolName: "read"}
	write := PermissionRuleValue{ToolName: "write"}
	if err := manager.Apply(PermissionUpdate{Destination: DestinationUser, Operation: "add", Behavior: PermissionBehaviorAllow, Rules: []PermissionRuleValue{read, write}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(PermissionUpdate{Destination: DestinationUser, Operation: "remove", Rules: []PermissionRuleValue{read}}); err != nil {
		t.Fatal(err)
	}
	if got := manager.GetRules(SourceUserSettings); len(got) != 1 || got[0].RuleValue != write {
		t.Fatalf("rules after remove = %#v", got)
	}
	if err := manager.Apply(PermissionUpdate{Destination: DestinationProject, Operation: "replace", Behavior: PermissionBehaviorDeny, Rules: []PermissionRuleValue{read}}); err != nil {
		t.Fatal(err)
	}
	if got := manager.GetRules(SourceProjectSettings); len(got) != 1 || got[0].RuleBehavior != PermissionBehaviorDeny {
		t.Fatalf("project rules = %#v", got)
	}
	if err := manager.Apply(PermissionUpdate{Destination: DestinationLocal, Operation: "add", Behavior: PermissionBehaviorAsk, Rules: []PermissionRuleValue{write}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(PermissionUpdate{Destination: "remote", Operation: "add"}); err == nil {
		t.Fatal("unknown update destination was accepted")
	}
	if err := manager.Apply(PermissionUpdate{Destination: DestinationUser, Operation: "merge"}); err == nil {
		t.Fatal("unknown update operation was accepted")
	}
}

func TestBrokerValidatesPolicyAndConfirmationFailures(t *testing.T) {
	for _, options := range []Options{
		{Mode: "invalid"},
		{Rules: []Rule{{Behavior: PermissionBehaviorAllow}}},
		{Rules: []Rule{{ID: "bad", Behavior: "sometimes"}}},
	} {
		if _, err := NewBroker(options); err == nil {
			t.Fatalf("invalid options were accepted: %#v", options)
		}
	}
	sentinel := errors.New("prompt unavailable")
	broker, err := NewBroker(Options{Confirmer: func(context.Context, Request) (Decision, error) {
		return Decision{}, sentinel
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Decide(nil, Request{Tool: "shell", Action: ActionExecute}); !errors.Is(err, sentinel) {
		t.Fatalf("confirmation error = %v", err)
	}

	broker, err = NewBroker(Options{
		Rules: []Rule{{ID: "ask-shell", Source: SourceUserSettings, Behavior: PermissionBehaviorAsk, Tool: "shell"}},
		Confirmer: func(context.Context, Request) (Decision, error) {
			return Decision{Behavior: PermissionBehaviorDeny}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := broker.Decide(context.Background(), Request{Tool: "shell", Action: ActionExecute})
	if err != nil || decision.Behavior != PermissionBehaviorDeny || !strings.Contains(decision.Reason, "not confirmed") {
		t.Fatalf("denied confirmation = %#v, %v", decision, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := broker.Decide(canceled, Request{Tool: "read", Action: ActionRead}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled decision error = %v", err)
	}
}

func TestAuditLogIsBoundedRedactedAndReturnsSnapshots(t *testing.T) {
	var nilLog *AuditLog
	nilLog.Record(AuditRecord{Tool: "ignored"})
	if nilLog.Records() != nil {
		t.Fatal("nil audit log returned records")
	}
	log := NewAuditLog(2)
	for index, tool := range []string{"first", "second", "api_key=secret-value"} {
		log.Record(AuditRecord{Tool: tool, Action: ActionRead, Reason: fmt.Sprintf("reason-%d", index)})
	}
	records := log.Records()
	if len(records) != 2 || records[0].Tool != "second" || strings.Contains(records[1].Tool, "secret-value") {
		t.Fatalf("records = %#v", records)
	}
	records[0].Tool = "mutated"
	if log.Records()[0].Tool != "second" {
		t.Fatal("Records returned mutable audit storage")
	}
	if NewAuditLog(0).limit != 1000 {
		t.Fatal("default audit limit was not applied")
	}
}

func TestResolvePathValidatesWorkspaceAndMissingDescendants(t *testing.T) {
	if _, err := ResolvePath("", "file"); err == nil {
		t.Fatal("empty workspace was accepted")
	}
	workspace := t.TempDir()
	if _, err := ResolvePath(workspace, ""); err == nil {
		t.Fatal("empty path was accepted")
	}
	resolved, err := ResolvePath(workspace, filepath.Join("missing", "child.txt"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Join(canonicalWorkspace, "missing", "child.txt") {
		t.Fatalf("resolved path = %q", resolved)
	}
	if err := validateRequestPaths("", []string{"file"}); err == nil {
		t.Fatal("filesystem request without workspace was accepted")
	}
	if _, err := ResolvePath(filepath.Join(workspace, "missing-workspace"), "file"); err == nil {
		t.Fatal("missing workspace was accepted")
	}
}
