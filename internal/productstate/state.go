// Package productstate projects validated product events into immutable snapshots.
package productstate

import "cyber-code/internal/productprotocol"

type ActiveRuntime struct {
	ID string `json:"id"`
}

type TaskState struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type State struct {
	ActiveRuntime                 *ActiveRuntime                                    `json:"activeRuntime"`
	Task                          *TaskState                                        `json:"task"`
	Plan                          *productprotocol.PlanState                        `json:"plan,omitempty"`
	Scope                         *productprotocol.ScopeSnapshot                    `json:"scope"`
	ControlLease                  *productprotocol.ControlLease                     `json:"controlLease"`
	HighestCommittedLeaseRevision int                                               `json:"highestCommittedLeaseRevision"`
	Agents                        map[string]productprotocol.AgentState             `json:"agents"`
	Timeline                      []productprotocol.Event                           `json:"timeline"`
	Approvals                     map[string]productprotocol.ApprovalState          `json:"approvals"`
	Findings                      map[string]productprotocol.FindingState           `json:"findings"`
	Evidence                      map[string]productprotocol.ImmutableEvidence      `json:"evidence"`
	EvidenceReferences            map[string]productprotocol.EvidenceReferenceState `json:"evidenceReferences,omitempty"`
	Artifacts                     map[string]productprotocol.ArtifactState          `json:"artifacts,omitempty"`
	ToolReceipts                  map[string]productprotocol.ToolReceiptState       `json:"toolReceipts,omitempty"`
	Interactions                  map[string]productprotocol.InteractionState       `json:"interactions,omitempty"`
	Terminals                     map[string]productprotocol.TerminalSessionState   `json:"terminals"`
	EditorDrafts                  map[string]productprotocol.EditorDraftState       `json:"editorDrafts"`
	AssetNodes                    map[string]productprotocol.AssetNodeState         `json:"assetNodes"`
	AssetEdges                    map[string]productprotocol.AssetEdgeState         `json:"assetEdges"`
	Report                        *productprotocol.ReportState                      `json:"report"`
	RawEvents                     []productprotocol.Event                           `json:"rawEvents"`
	CommittedCursor               int                                               `json:"committedCursor"`
	CanonicalEvents               map[string]string                                 `json:"canonicalEvents"`
}

func Initial() State {
	return State{
		Agents:             make(map[string]productprotocol.AgentState),
		Timeline:           make([]productprotocol.Event, 0),
		Approvals:          make(map[string]productprotocol.ApprovalState),
		Findings:           make(map[string]productprotocol.FindingState),
		Evidence:           make(map[string]productprotocol.ImmutableEvidence),
		EvidenceReferences: make(map[string]productprotocol.EvidenceReferenceState),
		Artifacts:          make(map[string]productprotocol.ArtifactState),
		ToolReceipts:       make(map[string]productprotocol.ToolReceiptState),
		Interactions:       make(map[string]productprotocol.InteractionState),
		Terminals:          make(map[string]productprotocol.TerminalSessionState),
		EditorDrafts:       make(map[string]productprotocol.EditorDraftState),
		AssetNodes:         make(map[string]productprotocol.AssetNodeState),
		AssetEdges:         make(map[string]productprotocol.AssetEdgeState),
		RawEvents:          make([]productprotocol.Event, 0),
		CanonicalEvents:    make(map[string]string),
	}
}
