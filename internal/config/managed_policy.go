package config

import (
	"fmt"
	"sort"
)

type ManagedPolicySettings struct {
	TenantID     string
	Revision     uint64
	TrustLevel   string
	AllowedTools []string
	DenyTools    []string
}

func ApplyManagedPolicy(target *Config, policy ManagedPolicySettings) error {
	if target == nil || policy.TenantID == "" || policy.Revision == 0 || policy.TrustLevel != "managed" || policy.Revision <= target.ManagedPolicyRevision || target.ManagedPolicyTenant != "" && target.ManagedPolicyTenant != policy.TenantID {
		return fmt.Errorf("managed policy is invalid or stale")
	}
	candidate := *target
	candidate.AllowedTools = append([]string(nil), target.AllowedTools...)
	candidate.DenyTools = append([]string(nil), target.DenyTools...)
	if target.AllowedTools == nil {
		candidate.AllowedTools = append([]string(nil), policy.AllowedTools...)
	} else if policy.AllowedTools != nil {
		candidate.AllowedTools = intersectStrings(target.AllowedTools, policy.AllowedTools)
	}
	candidate.DenyTools = unionStrings(target.DenyTools, policy.DenyTools)
	sort.Strings(candidate.AllowedTools)
	sort.Strings(candidate.DenyTools)
	candidate.TrustLevel = "managed"
	candidate.ManagedPolicyTenant = policy.TenantID
	candidate.ManagedPolicyRevision = policy.Revision
	if err := Validate(&candidate); err != nil {
		return err
	}
	*target = candidate
	return nil
}
