package vim

import "testing"

func TestVimTransitionModesMotionsOperatorsAndRepeat(t *testing.T) {
	state := NewVimState()
	persistent := NewPersistentState()
	transition := NewTransition(state, persistent)

	tests := []struct {
		key      string
		wantKind ActionKind
		wantMode Mode
		wantOp   Operator
		wantMove string
	}{
		{"你", ActionInsert, ModeInsert, "", ""},
		{"<Esc>", ActionSetMode, ModeNormal, "", ""},
		{"w", ActionMove, ModeNormal, "", "w"},
		{"d", ActionNone, ModeNormal, "", ""},
		{"w", ActionOperator, ModeNormal, OperatorDelete, "w"},
		{"v", ActionSetMode, ModeVisual, "", ""},
		{"l", ActionMove, ModeVisual, "", "l"},
		{"y", ActionOperator, ModeNormal, OperatorYank, "selection"},
		{"u", ActionUndo, ModeNormal, "", ""},
		{".", ActionRepeat, ModeNormal, "", ""},
	}

	for _, test := range tests {
		action, err := transition.ActionForKey(test.key)
		if err != nil {
			t.Fatalf("key %q: %v", test.key, err)
		}
		if action.Kind != test.wantKind || state.Mode != test.wantMode || action.Operator != test.wantOp || action.Motion != test.wantMove {
			t.Fatalf("key %q: action = %#v, mode = %q", test.key, action, state.Mode)
		}
	}
}

func TestVimTransitionCountFindAndEscape(t *testing.T) {
	state := &VimState{Mode: ModeNormal, Command: NewCommandState()}
	transition := NewTransition(state, NewPersistentState())

	for _, key := range []string{"2", "f"} {
		if _, err := transition.ActionForKey(key); err != nil {
			t.Fatal(err)
		}
	}
	action, err := transition.ActionForKey("界")
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionFind || action.Find != FindTypeF || action.Text != "界" || action.Count != 2 {
		t.Fatalf("find action = %#v", action)
	}
	if _, err := transition.ActionForKey("v"); err != nil {
		t.Fatal(err)
	}
	action, err = transition.ActionForKey("<Esc>")
	if err != nil || action.Kind != ActionSetMode || state.Mode != ModeNormal {
		t.Fatalf("escape action = %#v, mode = %q, error = %v", action, state.Mode, err)
	}
}

func TestVimTransitionCombinesOperatorCounts(t *testing.T) {
	for _, keys := range [][]string{{"2", "d", "3", "w"}, {"d", "6", "w"}} {
		state := &VimState{Mode: ModeNormal, Command: NewCommandState()}
		transition := NewTransition(state, NewPersistentState())
		var action Action
		for _, key := range keys {
			var err error
			action, err = transition.ActionForKey(key)
			if err != nil {
				t.Fatal(err)
			}
		}
		if action.Kind != ActionOperator || action.Operator != OperatorDelete || action.Motion != "w" || action.Count != 6 {
			t.Fatalf("keys %v: action = %#v", keys, action)
		}
	}
}

func TestVimTransitionBasicAndWordMotions(t *testing.T) {
	for _, motion := range []string{"h", "j", "k", "l", "w", "b", "e", "W", "B", "E"} {
		state := &VimState{Mode: ModeNormal, Command: NewCommandState()}
		action, err := NewTransition(state, NewPersistentState()).ActionForKey(motion)
		if err != nil || action.Kind != ActionMove || action.Motion != motion {
			t.Fatalf("motion %q: action = %#v, error = %v", motion, action, err)
		}
	}
}
