package permissions

import "fmt"

const (
	PermissionModeAcceptEdits PermissionMode = "accept-edits"
	PermissionModeBypass      PermissionMode = "bypass"
)

const (
	ActionRead    = "read"
	ActionWrite   = "write"
	ActionExecute = "execute"
	ActionDelete  = "delete"
	ActionNetwork = "network"
)

// Rule is one explicit policy decision. Empty Tool or Action fields match all
// requests. Project rules may only reduce permissions.
type Rule struct {
	ID           string
	Source       PermissionRuleSource
	Behavior     PermissionBehavior
	Tool         string
	Action       string
	AllowedTools []string
}

func validatePolicy(mode PermissionMode, modeSource PermissionRuleSource, rules []Rule) error {
	if mode == "" {
		mode = PermissionModeDefault
	}
	switch mode {
	case PermissionModeDefault, PermissionModePlan, PermissionModeAcceptEdits, PermissionModeBypass:
	default:
		return fmt.Errorf("unsupported permission mode %q", mode)
	}
	if mode == PermissionModeBypass && modeSource != SourceCliArg {
		return fmt.Errorf("bypass mode requires an explicit CLI source")
	}
	for _, rule := range rules {
		if rule.ID == "" {
			return fmt.Errorf("permission rule ID is required")
		}
		switch rule.Behavior {
		case PermissionBehaviorAllow, PermissionBehaviorDeny, PermissionBehaviorAsk:
		default:
			return fmt.Errorf("permission rule %q has unsupported behavior %q", rule.ID, rule.Behavior)
		}
		if rule.Source == SourceProjectSettings && rule.Behavior == PermissionBehaviorAllow {
			return fmt.Errorf("project rule %q cannot expand permissions", rule.ID)
		}
	}
	return nil
}

func ruleMatches(rule Rule, request Request) bool {
	if len(rule.AllowedTools) > 0 && containsTool(rule.AllowedTools, request.Tool) {
		return false
	}
	return (rule.Tool == "" || rule.Tool == request.Tool) &&
		(rule.Action == "" || rule.Action == request.Action)
}

func modeDefault(mode PermissionMode, action string) Decision {
	switch mode {
	case PermissionModePlan:
		if action == ActionRead {
			return Decision{Behavior: PermissionBehaviorAllow, Reason: "plan mode permits workspace reads"}
		}
		return Decision{Behavior: PermissionBehaviorDeny, Reason: "plan mode denies state-changing actions"}
	case PermissionModeAcceptEdits:
		if action == ActionRead || action == ActionWrite {
			return Decision{Behavior: PermissionBehaviorAllow, Reason: "accept-edits mode permits workspace reads and writes"}
		}
		return Decision{Behavior: PermissionBehaviorAsk, Reason: "accept-edits mode requires confirmation for this action"}
	case PermissionModeBypass:
		return Decision{Behavior: PermissionBehaviorAllow, Reason: "explicit CLI bypass mode"}
	default:
		if action == ActionRead {
			return Decision{Behavior: PermissionBehaviorAllow, Reason: "default mode permits workspace reads"}
		}
		return Decision{Behavior: PermissionBehaviorAsk, Reason: "default mode requires confirmation"}
	}
}
