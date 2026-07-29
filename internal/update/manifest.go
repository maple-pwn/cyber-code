package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"cyber-code/internal/product"
)

const (
	ManifestSchemaVersion = 1
	maxManifestArtifacts  = 64
	maxArtifactURLBytes   = 2048
)

type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Payload       string `json:"payload"`
	Signature     string `json:"signature"`
}

type Payload struct {
	SchemaVersion int        `json:"schema_version"`
	Product       string     `json:"product"`
	Version       string     `json:"version"`
	PublishedAt   string     `json:"published_at"`
	Artifacts     []Artifact `json:"artifacts"`
}

type Artifact struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// BuildEnvelope validates and signs a deterministic release payload.
func BuildEnvelope(payload Payload, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("build release envelope: invalid Ed25519 private key")
	}
	canonical, _, err := canonicalPayload(payload)
	if err != nil {
		return nil, err
	}
	envelope := Envelope{
		SchemaVersion: ManifestSchemaVersion,
		Payload:       base64.StdEncoding.EncodeToString(canonical),
		Signature:     base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
	}
	document, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode release envelope: %w", err)
	}
	if len(document) > maxMetadataBytes {
		return nil, fmt.Errorf("encode release envelope: document exceeds %d bytes", maxMetadataBytes)
	}
	return document, nil
}

// VerifyEnvelope authenticates a canonical envelope and selects one platform artifact.
func VerifyEnvelope(document []byte, publicKey ed25519.PublicKey, goos, goarch string) (Payload, Artifact, error) {
	if len(document) == 0 || len(document) > maxMetadataBytes {
		return Payload{}, Artifact{}, fmt.Errorf("verify release envelope: document size is invalid")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return Payload{}, Artifact{}, fmt.Errorf("verify release envelope: invalid Ed25519 public key")
	}
	var envelope Envelope
	if err := strictJSON(document, &envelope); err != nil {
		return Payload{}, Artifact{}, fmt.Errorf("decode release envelope: %w", err)
	}
	canonicalEnvelope, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(bytes.TrimSpace(document), canonicalEnvelope) {
		return Payload{}, Artifact{}, fmt.Errorf("verify release envelope: envelope is not canonical")
	}
	if envelope.SchemaVersion != ManifestSchemaVersion {
		return Payload{}, Artifact{}, fmt.Errorf("verify release envelope: unsupported schema version %d", envelope.SchemaVersion)
	}
	payloadBytes, err := base64.StdEncoding.Strict().DecodeString(envelope.Payload)
	if err != nil || len(payloadBytes) == 0 || len(payloadBytes) > maxMetadataBytes {
		return Payload{}, Artifact{}, fmt.Errorf("verify release envelope: invalid payload encoding")
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, payloadBytes, signature) {
		return Payload{}, Artifact{}, fmt.Errorf("verify release envelope: invalid signature")
	}
	var payload Payload
	if err := strictJSON(payloadBytes, &payload); err != nil {
		return Payload{}, Artifact{}, fmt.Errorf("decode release payload: %w", err)
	}
	canonical, normalized, err := canonicalPayload(payload)
	if err != nil {
		return Payload{}, Artifact{}, err
	}
	if !bytes.Equal(payloadBytes, canonical) {
		return Payload{}, Artifact{}, fmt.Errorf("verify release payload: payload is not canonical")
	}
	for _, artifact := range normalized.Artifacts {
		if artifact.GOOS == goos && artifact.GOARCH == goarch {
			return normalized, artifact, nil
		}
	}
	return Payload{}, Artifact{}, fmt.Errorf("verify release payload: no artifact for %s/%s", goos, goarch)
}

func canonicalPayload(payload Payload) ([]byte, Payload, error) {
	normalized := payload
	normalized.Artifacts = append([]Artifact(nil), payload.Artifacts...)
	if err := validatePayload(normalized); err != nil {
		return nil, Payload{}, err
	}
	sort.Slice(normalized.Artifacts, func(left, right int) bool {
		if normalized.Artifacts[left].GOOS == normalized.Artifacts[right].GOOS {
			return normalized.Artifacts[left].GOARCH < normalized.Artifacts[right].GOARCH
		}
		return normalized.Artifacts[left].GOOS < normalized.Artifacts[right].GOOS
	})
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, Payload{}, fmt.Errorf("encode release payload: %w", err)
	}
	return encoded, normalized, nil
}

func validatePayload(payload Payload) error {
	if payload.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("validate release payload: unsupported schema version %d", payload.SchemaVersion)
	}
	if payload.Product != product.Name {
		return fmt.Errorf("validate release payload: product must be %q", product.Name)
	}
	if len(payload.Version) == 0 || len(payload.Version) > 64 {
		return fmt.Errorf("validate release payload: version length is invalid")
	}
	if !validReleaseVersion(payload.Version) {
		return fmt.Errorf("validate release payload: version must use semantic version syntax")
	}
	if _, err := parseVersion(payload.Version); err != nil {
		return fmt.Errorf("validate release payload version: %w", err)
	}
	published, err := time.Parse(time.RFC3339, payload.PublishedAt)
	if err != nil || published.UTC().Format(time.RFC3339) != payload.PublishedAt {
		return fmt.Errorf("validate release payload: published_at must be canonical RFC3339 UTC")
	}
	if len(payload.Artifacts) == 0 || len(payload.Artifacts) > maxManifestArtifacts {
		return fmt.Errorf("validate release payload: artifact count must be between 1 and %d", maxManifestArtifacts)
	}
	platforms := make(map[string]struct{}, len(payload.Artifacts))
	for _, artifact := range payload.Artifacts {
		if !allowedManifestGOOS(artifact.GOOS) || !allowedManifestGOARCH(artifact.GOARCH) {
			return fmt.Errorf("validate release artifact: unsupported platform %s/%s", artifact.GOOS, artifact.GOARCH)
		}
		platform := artifact.GOOS + "/" + artifact.GOARCH
		if _, exists := platforms[platform]; exists {
			return fmt.Errorf("validate release artifact: duplicate platform %s", platform)
		}
		platforms[platform] = struct{}{}
		if len(artifact.URL) == 0 || len(artifact.URL) > maxArtifactURLBytes {
			return fmt.Errorf("validate release artifact %s: URL length is invalid", platform)
		}
		parsedURL, err := secureURL(artifact.URL)
		if err != nil || strings.TrimSpace(artifact.URL) != artifact.URL || parsedURL.Fragment != "" {
			return fmt.Errorf("validate release artifact %s: URL must be absolute HTTPS without user information or fragment", platform)
		}
		if len(artifact.SHA256) != 64 || strings.Trim(artifact.SHA256, "0123456789abcdef") != "" {
			return fmt.Errorf("validate release artifact %s: SHA-256 must be 64 lowercase hexadecimal characters", platform)
		}
		if artifact.Size <= 0 {
			return fmt.Errorf("validate release artifact %s: size must be positive", platform)
		}
	}
	return nil
}

func validReleaseVersion(value string) bool {
	if value == "" || strings.HasPrefix(value, "v") || strings.Count(value, "+") > 1 {
		return false
	}
	versionAndBuild := strings.SplitN(value, "+", 2)
	if len(versionAndBuild) == 2 && !validVersionIdentifiers(versionAndBuild[1], false) {
		return false
	}
	coreAndPrerelease := strings.SplitN(versionAndBuild[0], "-", 2)
	if len(coreAndPrerelease) == 2 && !validVersionIdentifiers(coreAndPrerelease[1], true) {
		return false
	}
	parts := strings.Split(coreAndPrerelease[0], ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !decimalIdentifier(part) || (len(part) > 1 && part[0] == '0') {
			return false
		}
	}
	return true
}

func validVersionIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, character := range identifier {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-') {
				return false
			}
		}
		if rejectNumericLeadingZero && len(identifier) > 1 && identifier[0] == '0' && decimalIdentifier(identifier) {
			return false
		}
	}
	return true
}

func decimalIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func strictJSON(document []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func allowedManifestGOOS(value string) bool {
	switch value {
	case "darwin", "linux", "windows":
		return true
	default:
		return false
	}
}

func allowedManifestGOARCH(value string) bool {
	switch value {
	case "amd64", "arm64":
		return true
	default:
		return false
	}
}
