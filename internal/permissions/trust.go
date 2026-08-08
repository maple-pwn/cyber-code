package permissions

import (
	"fmt"
	"sort"
	"strings"
)

type TrustLevel string

const (
	TrustTrusted   TrustLevel = "trusted"
	TrustSuggested TrustLevel = "suggested"
	TrustManaged   TrustLevel = "managed"
)

// TrustRules converts enterprise tool bounds into deny-only broker rules.
// Allowed tools still pass through the normal permission mode and confirmer;
// this policy can reduce authority but cannot grant it.
func TrustRules(level TrustLevel, allowedTools, deniedTools []string) ([]Rule, error) {
	switch level {
	case "", TrustTrusted, TrustSuggested, TrustManaged:
	default:
		return nil, fmt.Errorf("unsupported trust level %q", level)
	}
	allowed, err := normalizeToolSet(allowedTools)
	if err != nil {
		return nil, fmt.Errorf("allowed tools: %w", err)
	}
	denied, err := normalizeToolSet(deniedTools)
	if err != nil {
		return nil, fmt.Errorf("denied tools: %w", err)
	}
	rules := make([]Rule, 0, len(denied)+1)
	for _, tool := range denied {
		ruleTool := tool
		if tool == "*" {
			ruleTool = ""
		}
		rules = append(rules, Rule{ID: "managed-deny-" + tool, Source: SourcePolicySettings, Behavior: PermissionBehaviorDeny, Tool: ruleTool})
	}
	if level == TrustManaged && len(allowed) > 0 && !containsTool(allowed, "*") {
		rules = append(rules, Rule{ID: "managed-allowlist-boundary", Source: SourcePolicySettings, Behavior: PermissionBehaviorDeny, AllowedTools: append([]string(nil), allowed...)})
	}
	return rules, nil
}

func normalizeToolSet(values []string) ([]string, error) {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 || strings.ContainsAny(value, " \t\r\n") {
			return nil, fmt.Errorf("invalid tool name %q", value)
		}
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func containsTool(values []string, tool string) bool {
	for _, value := range values {
		if value == tool {
			return true
		}
	}
	return false
}
