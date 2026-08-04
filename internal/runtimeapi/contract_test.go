package runtimeapi_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"cyber-code/internal/runtimeapi"
)

func TestConformanceManifestFreezesRequiredCases(t *testing.T) {
	var manifest struct {
		SchemaVersion int      `json:"schemaVersion"`
		Cases         []string `json:"cases"`
	}
	readFixture(t, "manifest.json", &manifest)
	want := []string{
		"handshake-negotiation", "replay-after-cursor", "exact-duplicate",
		"gap-recovery", "snapshot-mismatch", "unknown-event", "command-rejection",
		"expired-approval", "stale-lease", "cancellation", "reconnect-during-active-execution",
	}
	if manifest.SchemaVersion != 1 || !reflect.DeepEqual(manifest.Cases, want) {
		t.Fatalf("unexpected conformance manifest: %#v", manifest)
	}
}

func TestNegotiateHandshake(t *testing.T) {
	var value struct {
		Request  runtimeapi.HandshakeRequest  `json:"request"`
		Response runtimeapi.HandshakeResponse `json:"response"`
	}
	readFixture(t, "handshake.json", &value)

	metadata, err := runtimeapi.NegotiateHandshake(value.Request, value.Response)
	if err != nil {
		t.Fatalf("negotiate handshake: %v", err)
	}
	if metadata.Mode != runtimeapi.SourceModeLocal || metadata.RuntimeID != value.Response.RuntimeID {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}

	value.Request.SupportedProtocolVersions = []int{2}
	if _, err := runtimeapi.NegotiateHandshake(value.Request, value.Response); err != runtimeapi.ErrIncompatible {
		t.Fatalf("expected incompatible, got %v", err)
	}
}

func TestValidateCommandReceipts(t *testing.T) {
	var value struct {
		Accepted struct {
			Envelope runtimeapi.CommandEnvelope `json:"envelope"`
			Receipt  runtimeapi.CommandReceipt  `json:"receipt"`
		} `json:"accepted"`
		Rejected []struct {
			Name     string                     `json:"name"`
			Envelope runtimeapi.CommandEnvelope `json:"envelope"`
			Receipt  runtimeapi.CommandReceipt  `json:"receipt"`
		} `json:"rejected"`
	}
	readFixture(t, "commands.json", &value)

	if err := runtimeapi.ValidateCommandEnvelope(value.Accepted.Envelope); err != nil {
		t.Fatalf("validate accepted envelope: %v", err)
	}
	if err := runtimeapi.ValidateCommandReceipt(value.Accepted.Receipt, value.Accepted.Envelope); err != nil {
		t.Fatalf("validate accepted receipt: %v", err)
	}
	for _, item := range value.Rejected {
		if err := runtimeapi.ValidateCommandEnvelope(item.Envelope); err != nil {
			t.Fatalf("validate %s envelope: %v", item.Name, err)
		}
		if err := runtimeapi.ValidateCommandReceipt(item.Receipt, item.Envelope); err != nil {
			t.Fatalf("validate %s receipt: %v", item.Name, err)
		}
	}

	mismatched := value.Accepted.Receipt
	mismatched.IdempotencyKey = "cmd-other"
	if err := runtimeapi.ValidateCommandReceipt(mismatched, value.Accepted.Envelope); err != runtimeapi.ErrReceiptIdempotencyMismatch {
		t.Fatalf("expected receipt mismatch, got %v", err)
	}
}

func TestValidateCommandEnvelopeRejectsTrailingJSON(t *testing.T) {
	envelope := runtimeapi.CommandEnvelope{
		IdempotencyKey: "cmd-trailing-json",
		Command:        json.RawMessage(`{"type":"task.cancel"} trailing`),
	}
	if err := runtimeapi.ValidateCommandEnvelope(envelope); err != runtimeapi.ErrInvalidCommandEnvelope {
		t.Fatalf("expected invalid envelope, got %v", err)
	}
}

func TestValidateCommandEnvelopeRejectsForgedAuthorityFields(t *testing.T) {
	for _, command := range []string{
		`{"type":"task.cancel","eventId":"event-forged"}`,
		`{"type":"task.cancel","cursor":99}`,
	} {
		envelope := runtimeapi.CommandEnvelope{
			IdempotencyKey: "cmd-forged-authority",
			Command:        json.RawMessage(command),
		}
		if err := runtimeapi.ValidateCommandEnvelope(envelope); err != runtimeapi.ErrInvalidCommandEnvelope {
			t.Fatalf("expected invalid envelope for %s, got %v", command, err)
		}
	}
}

func readFixture(t *testing.T, name string, target any) {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	path := filepath.Join(filepath.Dir(current), "..", "..", "tests", "fixtures", "runtime-conformance", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
}
