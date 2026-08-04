package mission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

func TestGoldenLayouts(t *testing.T) {
	cases := []struct {
		name    string
		state   productstate.State
		options Options
	}{
		{name: "80x24", state: missionState(), options: Options{Width: 80, Height: 24, Demo: true, Runtime: "local", Connection: "live"}},
		{name: "100x30", state: missionState(), options: Options{Width: 100, Height: 30, Demo: true, Runtime: "local", Connection: "live"}},
		{name: "120x40", state: missionState(), options: Options{Width: 120, Height: 40, Demo: true, Runtime: "local", Connection: "live"}},
		{name: "160x50", state: missionState(), options: Options{Width: 160, Height: 50, Demo: true, Runtime: "local", Connection: "live"}},
		{name: "no-color", state: missionState(), options: Options{Width: 100, Height: 30, Demo: true, Runtime: "local", Connection: "live", NoColor: true}},
		{name: "cjk", state: cjkGoldenState(), options: Options{Width: 80, Height: 24, Demo: true, Runtime: "local", Connection: "live"}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := ansi.Strip(NewModel(test.state, test.options).View()) + "\n"
			path := filepath.Join("testdata", test.name+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s; run UPDATE_GOLDEN=1 go test ./internal/ui/mission -run TestGoldenLayouts", test.name)
			}
		})
	}
}

func cjkGoldenState() productstate.State {
	state := missionState()
	state.Task.Title = "東方服务安全审查"
	state.Scope.Workspace = "/工作区/东方服务"
	state.Scope.Targets = []string{"登录接口", "管理后台"}
	state.Agents["agent-1"] = productprotocol.AgentState{
		ID: "agent-1", Name: "侦察 Agent", Status: "running", CurrentAction: "检查 e\u0301ndpoint 与 👩🏽‍💻 会话",
	}
	state.Evidence["evidence-1"] = productprotocol.ImmutableEvidence{
		ID: "evidence-1", TaskID: "task-1", Kind: "http", Summary: "发现管理端点未授权访问", Data: map[string]any{},
	}
	state.Findings["finding-1"] = productprotocol.FindingState{
		ID: "finding-1", Title: "管理端点访问控制", Severity: "high", Status: "verifying", Confidence: "medium", EvidenceIDs: []string{"evidence-1"},
	}
	state.Timeline[0].Payload = []byte(`{"title":"東方服务安全审查"}`)
	state.Timeline = append(state.Timeline, missionEvent(2, "evidence.committed", `{"evidence":{"id":"evidence-1","taskId":"task-1","kind":"http","summary":"发现管理端点未授权访问","data":{}}}`))
	state.Approvals["approval-1"] = productprotocol.ApprovalState{ApprovalChallenge: productprotocol.ApprovalChallenge{
		ID: "approval-1", AgentID: "agent-1", Action: "验证", Target: "管理后台", ParameterDigest: strings.Repeat("ab", 16), Risk: "medium", ExpiresAt: "2026-08-03T12:10:00Z",
	}}
	return state
}
