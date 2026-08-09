package config

import (
	"reflect"
	"testing"
)

func TestApplyManagedPolicyCanOnlyReduceAuthority(t *testing.T) {
	configuration := Default()
	configuration.TrustLevel = "suggested"
	configuration.AllowedTools = []string{"read_file", "shell", "edit_file"}
	configuration.DenyTools = []string{"delete_file"}
	err := ApplyManagedPolicy(&configuration, ManagedPolicySettings{TenantID: "tenant-a", Revision: 3, TrustLevel: "managed", AllowedTools: []string{"read_file", "shell"}, DenyTools: []string{"shell"}})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.TrustLevel != "managed" || !reflect.DeepEqual(configuration.AllowedTools, []string{"read_file", "shell"}) || !reflect.DeepEqual(configuration.DenyTools, []string{"delete_file", "shell"}) || configuration.ManagedPolicyRevision != 3 {
		t.Fatalf("managed configuration = %#v", configuration)
	}
	if err := ApplyManagedPolicy(&configuration, ManagedPolicySettings{TenantID: "tenant-a", Revision: 2, TrustLevel: "trusted"}); err == nil {
		t.Fatal("policy rollback or trust relaxation was accepted")
	}
}
