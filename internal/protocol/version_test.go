package protocol

import (
	"reflect"
	"testing"
)

func TestNegotiateVersionRejectsMajorAndDowngradesMinorCapabilities(t *testing.T) {
	if _, err := Negotiate(VersionInfo{Major: 1, Minor: 1}, VersionInfo{Major: 2, Minor: 0}, []string{"base"}, []string{"base"}); err == nil {
		t.Fatal("incompatible major version was accepted")
	}
	negotiated, err := Negotiate(
		VersionInfo{Major: 1, Minor: 1}, VersionInfo{Major: 1, Minor: 0},
		[]string{"base", "permissions", "ide-context", "diff"}, []string{"base", "permissions", "ide-context"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if negotiated.Version != (VersionInfo{Major: 1, Minor: 0}) || !reflect.DeepEqual(negotiated.Capabilities, []string{"base", "permissions"}) {
		t.Fatalf("negotiated = %#v", negotiated)
	}
}
