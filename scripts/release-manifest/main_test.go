package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	updatepkg "cyber-code/internal/update"
	"gopkg.in/yaml.v3"
)

func TestManifestCommandBuildsDeterministicSignedEnvelope(t *testing.T) {
	directory := t.TempDir()
	linuxPath := filepath.Join(directory, "cyber-code-linux-amd64")
	windowsPath := filepath.Join(directory, "cyber-code-windows-amd64.exe")
	if err := os.WriteFile(linuxPath, []byte("linux artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowsPath, []byte("windows artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))
	keyFile := filepath.Join(directory, "signing.key")
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(privateKey.Seed())), 0o600); err != nil {
		t.Fatal(err)
	}
	baseArgs := []string{
		"manifest", "--version", "2.2.0", "--published-at", "2026-07-29T00:00:00Z",
		"--base-url", "https://downloads.example.test/releases/2.2.0", "--key-file", keyFile,
	}
	firstArgs := append(append([]string{}, baseArgs...), "--artifact", "windows/amd64="+windowsPath, "--artifact", "linux/amd64="+linuxPath)
	secondArgs := append(append([]string{}, baseArgs...), "--artifact", "linux/amd64="+linuxPath, "--artifact", "windows/amd64="+windowsPath)
	var first, second bytes.Buffer
	if err := run(firstArgs, &first, &bytes.Buffer{}, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if err := run(secondArgs, &second, &bytes.Buffer{}, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("manifest output depends on artifact order:\n%s\n%s", first.String(), second.String())
	}
	payload, artifact, err := updatepkg.VerifyEnvelope(bytes.TrimSpace(first.Bytes()), privateKey.Public().(ed25519.PublicKey), "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("linux artifact"))
	if artifact.URL != "https://downloads.example.test/releases/2.2.0/cyber-code-linux-amd64" || artifact.SHA256 != fmt.Sprintf("%x", digest) || artifact.Size != int64(len("linux artifact")) {
		t.Fatalf("payload=%#v artifact=%#v", payload, artifact)
	}
	if strings.Contains(first.String(), base64.StdEncoding.EncodeToString(privateKey.Seed())) {
		t.Fatal("manifest output contains the signing seed")
	}
}

func TestPublicKeyCommandReadsRestrictedEnvironmentSource(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize))
	encodedSeed := base64.StdEncoding.EncodeToString(privateKey.Seed())
	var output bytes.Buffer
	err := run([]string{"public-key"}, &output, &bytes.Buffer{}, func(name string) string {
		if name == signingKeyEnvironment {
			return encodedSeed
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	want := base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey)) + "\n"
	if output.String() != want {
		t.Fatalf("public key output = %q, want %q", output.String(), want)
	}
}

func TestReleaseManifestRejectsInvalidInputsWithoutLeakingKey(t *testing.T) {
	directory := t.TempDir()
	artifact := filepath.Join(directory, "artifact")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "private-key-material-that-must-not-leak"
	keyFile := filepath.Join(directory, "bad.key")
	if err := os.WriteFile(keyFile, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"manifest", "--version", "2.2.0", "--published-at", "2026-07-29T00:00:00Z", "--base-url", "https://downloads.example.test", "--key-file", keyFile}
	tests := [][]string{
		append(append([]string{}, base...), "--artifact", "invalid="+artifact),
		append(append([]string{}, base...), "--artifact", "linux/amd64="),
		append(append([]string{}, base...), "--artifact", "linux/amd64="+filepath.Join(directory, "missing")),
		{"manifest", "--version", "2.2.0", "--published-at", "2026-07-29T00:00:00Z", "--base-url", "http://downloads.example.test", "--artifact", "linux/amd64=" + artifact},
	}
	for _, args := range tests {
		if err := run(args, &bytes.Buffer{}, &bytes.Buffer{}, func(string) string { return secret }); err == nil {
			t.Fatalf("invalid arguments were accepted: %v", args)
		} else if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked signing material: %v", err)
		}
	}
	if err := run([]string{"public-key", "--key-file", keyFile}, &bytes.Buffer{}, &bytes.Buffer{}, func(string) string { return "" }); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe key error = %v", err)
	}
}

func TestLoadSigningKeyRejectsInconsistentPrivateKey(t *testing.T) {
	raw := bytes.Repeat([]byte{4}, ed25519.PrivateKeySize)
	encoded := base64.StdEncoding.EncodeToString(raw)
	if _, err := loadSigningKey("", func(string) string { return encoded }); err == nil || strings.Contains(err.Error(), encoded) {
		t.Fatalf("inconsistent private key error = %v", err)
	}
}

func TestValidateCommandRejectsUnsafeLinkerInputs(t *testing.T) {
	valid := []string{"validate", "--version", "2.2.0-rc.1+build.7", "--base-url", "https://downloads.example.test/releases/2.2.0///"}
	var output bytes.Buffer
	if err := run(valid, &output, &bytes.Buffer{}, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if output.String() != "https://downloads.example.test/releases/2.2.0\n" {
		t.Fatalf("validated base URL = %q", output.String())
	}

	unsafe := [][]string{
		{"validate", "--version", "2.2.0\n-X injected=value", "--base-url", "https://downloads.example.test"},
		{"validate", "--version", "2.2.0", "--base-url", "https://downloads.example.test/path with-space"},
		{"validate", "--version", "2.2.0", "--base-url", "https://downloads.example.test/'quoted"},
		{"validate", "--version", "2.2.0", "--base-url", `https://downloads.example.test/\escape`},
	}
	for _, args := range unsafe {
		if err := run(args, &bytes.Buffer{}, &bytes.Buffer{}, func(string) string { return "" }); err == nil {
			t.Fatalf("unsafe release input was accepted: %q", args)
		}
	}
}

func TestReleaseBundleWorkflowIsManualReadOnlyAndDoesNotPublish(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "release-bundle.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&workflow); err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	triggers, ok := workflow["on"].(map[string]any)
	if !ok || len(triggers) != 1 || triggers["workflow_dispatch"] == nil {
		t.Fatalf("workflow triggers = %#v", workflow["on"])
	}
	permissions, ok := workflow["permissions"].(map[string]any)
	if !ok || len(permissions) != 1 || permissions["contents"] != "read" {
		t.Fatalf("workflow permissions = %#v", workflow["permissions"])
	}
	text := string(content)
	for _, required := range []string{"GOOS=linux", "GOOS=windows", "GOOS=darwin", "actions/upload-artifact@v4", "CYBER_CODE_UPDATE_SIGNING_KEY"} {
		if !strings.Contains(text, required) {
			t.Fatalf("workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"gh release", "actions/create-release", "curl --upload", "contents: write"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("workflow contains publishing behavior %q", forbidden)
		}
	}
	jobs := workflow["jobs"].(map[string]any)
	bundle := jobs["bundle"].(map[string]any)
	steps := bundle["steps"].([]any)
	foundBuildStep := false
	secretSteps := make([]string, 0, 2)
	for _, rawStep := range steps {
		step := rawStep.(map[string]any)
		environment, _ := step["env"].(map[string]any)
		if _, present := environment["CYBER_CODE_UPDATE_SIGNING_KEY"]; present {
			name, _ := step["name"].(string)
			secretSteps = append(secretSteps, name)
		}
		if step["name"] == "Build release binaries" {
			foundBuildStep = true
			if _, present := environment["CYBER_CODE_UPDATE_SIGNING_KEY"]; present {
				t.Fatal("release build step must not receive the private signing key")
			}
			if command, _ := step["run"].(string); !strings.Contains(command, "release-manifest validate") {
				t.Fatal("release build does not validate linker inputs with the Go release tool")
			}
		}
	}
	if !foundBuildStep {
		t.Fatal("release workflow is missing the release binary build step")
	}
	if fmt.Sprint(secretSteps) != "[Derive update public key Sign release manifest]" {
		t.Fatalf("private signing key is exposed to unexpected workflow steps: %v", secretSteps)
	}
}
