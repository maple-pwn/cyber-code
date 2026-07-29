package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildEnvelopeIsDeterministicAndVerifySelectsPlatform(t *testing.T) {
	privateKey := manifestTestPrivateKey()
	payload := validManifestPayload()
	first, err := BuildEnvelope(payload, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	payload.Artifacts[0], payload.Artifacts[1] = payload.Artifacts[1], payload.Artifacts[0]
	second, err := BuildEnvelope(payload, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("envelope changed with artifact input order:\n%s\n%s", first, second)
	}
	verified, artifact, err := VerifyEnvelope(first, privateKey.Public().(ed25519.PublicKey), "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if verified.Product != "cyber-code" || verified.Version != "2.2.0" || artifact.GOOS != "windows" || artifact.GOARCH != "amd64" || artifact.Size != 42 {
		t.Fatalf("verified=%#v artifact=%#v", verified, artifact)
	}
	if verified.Artifacts[0].GOOS != "linux" || verified.Artifacts[1].GOOS != "windows" {
		t.Fatalf("artifacts were not canonicalized: %#v", verified.Artifacts)
	}
}

func TestVerifyEnvelopeRejectsTamperingUnknownFieldsAndNonCanonicalPayload(t *testing.T) {
	privateKey := manifestTestPrivateKey()
	document, err := BuildEnvelope(validManifestPayload(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	var envelope Envelope
	if err := json.Unmarshal(document, &envelope); err != nil {
		t.Fatal(err)
	}

	tests := map[string][]byte{}
	tamperedPayload := envelope
	rawPayload, _ := base64.StdEncoding.DecodeString(tamperedPayload.Payload)
	rawPayload[0] ^= 1
	tamperedPayload.Payload = base64.StdEncoding.EncodeToString(rawPayload)
	tests["payload"] = mustJSON(t, tamperedPayload)
	tamperedSignature := envelope
	signature, _ := base64.StdEncoding.DecodeString(tamperedSignature.Signature)
	signature[0] ^= 1
	tamperedSignature.Signature = base64.StdEncoding.EncodeToString(signature)
	tests["signature"] = mustJSON(t, tamperedSignature)
	unknownSchema := envelope
	unknownSchema.SchemaVersion = 2
	tests["unknown envelope schema"] = mustJSON(t, unknownSchema)
	tests["unknown envelope field"] = []byte(strings.TrimSuffix(string(document), "}") + `,"extra":true}`)
	tests["duplicate envelope field"] = []byte(strings.Replace(string(document), `{"schema_version":1`, `{"schema_version":1,"schema_version":1`, 1))

	unknownPayload := []byte(`{"schema_version":1,"product":"cyber-code","version":"2.2.0","published_at":"2026-07-29T00:00:00Z","artifacts":[{"goos":"linux","goarch":"amd64","url":"https://downloads.example.test/cyber-code-linux","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":42}],"extra":true}`)
	tests["unknown payload field"] = signedRawEnvelope(t, unknownPayload, privateKey)
	nonCanonical := []byte(`{ "schema_version":1,"product":"cyber-code","version":"2.2.0","published_at":"2026-07-29T00:00:00Z","artifacts":[{"goos":"linux","goarch":"amd64","url":"https://downloads.example.test/cyber-code-linux","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":42}]}`)
	tests["non-canonical payload"] = signedRawEnvelope(t, nonCanonical, privateKey)

	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := VerifyEnvelope(candidate, privateKey.Public().(ed25519.PublicKey), "linux", "amd64"); err == nil {
				t.Fatal("invalid envelope was accepted")
			}
		})
	}
}

func TestManifestValidationRejectsUnsafeOrAmbiguousPayloads(t *testing.T) {
	privateKey := manifestTestPrivateKey()
	tests := map[string]func(*Payload){
		"schema":       func(payload *Payload) { payload.SchemaVersion = 2 },
		"product":      func(payload *Payload) { payload.Product = "other" },
		"version":      func(payload *Payload) { payload.Version = "latest" },
		"bad semver":   func(payload *Payload) { payload.Version = "2.2.0-???" },
		"published":    func(payload *Payload) { payload.PublishedAt = "2026-07-29 00:00:00" },
		"unknown os":   func(payload *Payload) { payload.Artifacts[0].GOOS = "plan9" },
		"unknown arch": func(payload *Payload) { payload.Artifacts[0].GOARCH = "mips" },
		"duplicate": func(payload *Payload) {
			payload.Artifacts[1].GOOS, payload.Artifacts[1].GOARCH = payload.Artifacts[0].GOOS, payload.Artifacts[0].GOARCH
		},
		"http url":    func(payload *Payload) { payload.Artifacts[0].URL = "http://downloads.example.test/file" },
		"url user":    func(payload *Payload) { payload.Artifacts[0].URL = "https://user@downloads.example.test/file" },
		"url spaces":  func(payload *Payload) { payload.Artifacts[0].URL = " https://downloads.example.test/file" },
		"hash case":   func(payload *Payload) { payload.Artifacts[0].SHA256 = strings.Repeat("A", 64) },
		"hash length": func(payload *Payload) { payload.Artifacts[0].SHA256 = "abcd" },
		"zero size":   func(payload *Payload) { payload.Artifacts[0].Size = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			payload := validManifestPayload()
			mutate(&payload)
			if _, err := BuildEnvelope(payload, privateKey); err == nil {
				t.Fatal("invalid payload was accepted")
			}
		})
	}
	payload := validManifestPayload()
	payload.Artifacts = make([]Artifact, maxManifestArtifacts+1)
	for index := range payload.Artifacts {
		payload.Artifacts[index] = Artifact{GOOS: "linux", GOARCH: "amd64", URL: "https://downloads.example.test/file", SHA256: strings.Repeat("a", 64), Size: 1}
	}
	if _, err := BuildEnvelope(payload, privateKey); err == nil {
		t.Fatal("oversized artifact list was accepted")
	}
}

func TestVerifyEnvelopeRequiresCurrentPlatformAndBoundsInput(t *testing.T) {
	privateKey := manifestTestPrivateKey()
	document, err := BuildEnvelope(validManifestPayload(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyEnvelope(document, privateKey.Public().(ed25519.PublicKey), "darwin", "arm64"); err == nil {
		t.Fatal("manifest without the current platform was accepted")
	}
	oversized := append(document, bytes.Repeat([]byte(" "), maxMetadataBytes-len(document)+1)...)
	if _, _, err := VerifyEnvelope(oversized, privateKey.Public().(ed25519.PublicKey), "linux", "amd64"); err == nil {
		t.Fatal("oversized envelope was accepted")
	}
}

func validManifestPayload() Payload {
	return Payload{
		SchemaVersion: ManifestSchemaVersion,
		Product:       "cyber-code",
		Version:       "2.2.0",
		PublishedAt:   "2026-07-29T00:00:00Z",
		Artifacts: []Artifact{
			{GOOS: "windows", GOARCH: "amd64", URL: "https://downloads.example.test/cyber-code.exe", SHA256: strings.Repeat("b", 64), Size: 42},
			{GOOS: "linux", GOARCH: "amd64", URL: "https://downloads.example.test/cyber-code", SHA256: strings.Repeat("a", 64), Size: 41},
		},
	}
}

func manifestTestPrivateKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func signedRawEnvelope(t *testing.T, payload []byte, privateKey ed25519.PrivateKey) []byte {
	t.Helper()
	return mustJSON(t, Envelope{
		SchemaVersion: ManifestSchemaVersion,
		Payload:       base64.StdEncoding.EncodeToString(payload),
		Signature:     base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	})
}
