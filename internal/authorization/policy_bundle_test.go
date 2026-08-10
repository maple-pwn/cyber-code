package authorization

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManagedPolicyVerifierChecksSignatureTenantExpiryAndRevision(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	verifier, err := NewManagedPolicyVerifier(ManagedPolicyVerifierOptions{Issuer: "https://policy.example.test", TenantID: "tenant-a", Keys: map[string]ed25519.PublicKey{"key-1": publicKey}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	payload := ManagedPolicy{Issuer: "https://policy.example.test", TenantID: "tenant-a", Revision: 1, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), TrustLevel: "managed", AllowedTools: []string{"read_file", "shell"}, DenyTools: []string{"shell"}}
	envelope, err := SignManagedPolicy("key-1", privateKey, payload)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(envelope)
	installed, err := verifier.VerifyAndInstall(data)
	if err != nil || installed.Revision != 1 {
		t.Fatalf("installed=%#v err=%v", installed, err)
	}
	if _, err := verifier.VerifyAndInstall(data); err != ErrPolicyRollback {
		t.Fatalf("rollback error=%v", err)
	}

	tampered := envelope
	tampered.Payload = append([]byte(nil), envelope.Payload...)
	tampered.Payload[len(tampered.Payload)-2] ^= 1
	tamperedData, _ := json.Marshal(tampered)
	if _, err := verifier.VerifyAndInstall(tamperedData); err == nil {
		t.Fatal("tampered managed policy was accepted")
	}

	expired := payload
	expired.Revision = 2
	expired.ExpiresAt = now.Add(-time.Second)
	expiredEnvelope, _ := SignManagedPolicy("key-1", privateKey, expired)
	expiredData, _ := json.Marshal(expiredEnvelope)
	if _, err := verifier.VerifyAndInstall(expiredData); err == nil {
		t.Fatal("expired managed policy was accepted")
	}
}

func TestManagedPolicyVerifierSupportsPinnedKeyRotation(t *testing.T) {
	oldPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	newPublic, newPrivate, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	verifier, err := NewManagedPolicyVerifier(ManagedPolicyVerifierOptions{Issuer: "https://policy.example.test", TenantID: "tenant-a", Keys: map[string]ed25519.PublicKey{"old": oldPublic, "new": newPublic}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	envelope, _ := SignManagedPolicy("new", newPrivate, ManagedPolicy{Issuer: "https://policy.example.test", TenantID: "tenant-a", Revision: 7, IssuedAt: now, ExpiresAt: now.Add(time.Hour), TrustLevel: "managed"})
	data, _ := json.Marshal(envelope)
	if _, err := verifier.VerifyAndInstall(data); err != nil {
		t.Fatal(err)
	}
}

func TestManagedPolicyFetcherUsesValidCachedRevisionDuringOutage(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	verifier, _ := NewManagedPolicyVerifier(ManagedPolicyVerifierOptions{Issuer: "https://policy.example.test", TenantID: "tenant-a", Keys: map[string]ed25519.PublicKey{"key-1": publicKey}, Now: func() time.Time { return now }})
	envelope, _ := SignManagedPolicy("key-1", privateKey, ManagedPolicy{Issuer: "https://policy.example.test", TenantID: "tenant-a", Revision: 1, IssuedAt: now, ExpiresAt: now.Add(time.Hour), TrustLevel: "managed"})
	data, _ := json.Marshal(envelope)
	failing := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if failing {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write(data)
	}))
	defer server.Close()
	fetcher, err := NewManagedPolicyFetcher(server.URL, verifier, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first, err := fetcher.Fetch(t.Context())
	if err != nil || first.Revision != 1 {
		t.Fatalf("first fetch=%#v err=%v", first, err)
	}
	failing = true
	cached, err := fetcher.Fetch(t.Context())
	if err != nil || cached.Revision != first.Revision {
		t.Fatalf("cached fetch=%#v err=%v", cached, err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := fetcher.Fetch(t.Context()); err == nil {
		t.Fatal("expired cached policy was used")
	}
}
